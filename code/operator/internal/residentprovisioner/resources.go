package residentprovisioner

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const runtimeName = "agent-runtime"
const triageWorkflow = "resident-issue-triage"
const agentCredentials = "agent-credentials"
const readyState = "ready"
const failedState = "failed"

const generationAnnotation = "aioutfitter.com/hosted-generation"
const requestAnnotation = "aioutfitter.com/hosted-request"

const workspaceAnnotation = "aioutfitter.com/hosted-workspace"
const managedLabel = "aioutfitter.com/hosted-managed"
const repositoriesAnnotation = "aioutfitter.com/hosted-repositories"
const displayAnnotation = "aioutfitter.com/resident-display-name"

//go:embed runtime-setup.cjs
var runtimeSetup string

//go:embed gh-wrapper.cjs
var ghWrapper string

func names(workspace string) (string, string, string) {
	base := "hosted-" + strings.ReplaceAll(workspace, ":", "-")
	return base, base + "-pm", base + "-eng"
}
func metadata(name, namespace, workspace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{managedLabel: "true"}, Annotations: map[string]string{workspaceAnnotation: workspace}}
}
func owned(obj client.Object, workspace string) bool {
	return obj.GetLabels()[managedLabel] == "true" && obj.GetAnnotations()[workspaceAnnotation] == workspace
}

func (s *Server) desired(r Request) []client.Object {
	workspace := r.Workspace.ID
	orgName, pmName, engName := names(workspace)
	repositoryJSON, _ := json.Marshal(r.Repositories)
	org := &api.Organization{ObjectMeta: metadata(orgName, "", workspace), Spec: api.OrganizationSpec{DisplayName: r.Workspace.Login, CredentialSecretName: "organization-credentials", AgentCatalogs: []api.AgentCatalog{{Name: "residents", GitHub: ptr.To(s.config.CatalogRepository), Revision: ptr.To(s.config.CatalogRevision)}}}}
	org.Annotations[repositoriesAnnotation] = string(repositoryJSON)
	org.Annotations["aioutfitter.com/github-installation"] = fmt.Sprint(r.InstallationID)
	objects := []client.Object{
		&corev1.Namespace{ObjectMeta: metadata("org-"+orgName, "", workspace)},
		&corev1.Secret{ObjectMeta: metadata("organization-credentials", "org-"+orgName, workspace), Data: map[string][]byte{}}, org,
	}
	wrapperJSON, _ := json.Marshal(ghWrapper)
	script := "node <<'OUTFITTER_RESIDENT_SETUP'\nconst ghWrapper = " + string(wrapperJSON) + ";\n" + runtimeSetup + "\nOUTFITTER_RESIDENT_SETUP\n"
	for _, role := range []struct {
		name, display, profile, token string
		manager                       bool
	}{{pmName, r.ProjectManagerName, "resident-project-manager", r.ProjectManagerToken, true}, {engName, r.EngineerName, "resident-engineer", r.EngineerToken, false}} {
		namespace := "agent-" + role.name
		data := map[string][]byte{
			"OUTFITTER_RESIDENT_TOKEN": []byte(role.token), "OUTFITTER_SERVICE_BASE_URL": []byte(r.ServiceBaseURL), "OUTFITTER_SELECTED_MODEL": []byte(s.config.Model),
			"OUTFITTER_REPOSITORIES_JSON": repositoryJSON, "OUTFITTER_RUNTIME_PATH": []byte(s.config.RuntimePath), "PATH": []byte("/workspace/.hosted/bin:" + s.config.RuntimePath),
			"CHANNELS_TASK_STORE_PATH":   []byte("/workspace/.channels/task-plane"),
			"OUTFITTER_HOSTED_INFERENCE": []byte("0"), "OUTFITTER_TELEMETRY": []byte("0"),
		}
		if role.manager {
			data["a2a-credentials.json"], _ = json.Marshal(map[string]any{"credentials": []map[string]string{{"token": r.TaskToken, "principal": "hosted:" + workspace}}})
		}
		encoded, _ := json.Marshal(data)
		digest := sha256.Sum256(encoded)
		agent := &api.Agent{ObjectMeta: metadata(role.name, "", workspace), Spec: api.AgentSpec{
			Memberships: []api.Membership{{Organization: orgName}}, Image: s.config.RuntimeImage, Profile: api.AgentProfile{Agent: role.profile, Harness: "pi", Model: "outfitter/" + s.config.Model}, CredentialSecretName: agentCredentials,
			Channels: []string{"off"}, CatalogSync: &api.CatalogSyncSpec{Enabled: true}, Workspace: s.config.Workspace,
			Setup: []api.SetupStep{{Name: "hosted-runtime", Script: "# credential revision " + hex.EncodeToString(digest[:]) + "\n" + script}},
		}}
		agent.Annotations[displayAnnotation] = role.display
		if role.manager {
			agent.Spec.TaskPlane = &api.AgentTaskPlaneSpec{Workflow: triageWorkflow}
		}
		objects = append(objects, &corev1.Namespace{ObjectMeta: metadata(namespace, "", workspace)}, &corev1.Secret{ObjectMeta: metadata(agentCredentials, namespace, workspace), Data: data}, agent)
		if role.manager {
			selector := map[string]string{"app.kubernetes.io/name": runtimeName, "app.kubernetes.io/instance": role.name}
			objects = append(objects, &corev1.Service{ObjectMeta: metadata(runtimeName, namespace, workspace), Spec: corev1.ServiceSpec{Selector: selector, Ports: []corev1.ServicePort{{Name: "a2a", Port: 8788, TargetPort: intstr.FromInt32(8788)}}}},
				&networkingv1.NetworkPolicy{ObjectMeta: metadata("hosted-manager-intake", namespace, workspace), Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: selector}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": s.config.OperatorNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"control-plane": "controller-manager"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(8788))}}}}}})
		}
	}
	encodedRequest, _ := json.Marshal(r)
	requestDigest := sha256.Sum256(encodedRequest)
	for _, obj := range objects {
		annotations := obj.GetAnnotations()
		annotations[generationAnnotation] = strconv.FormatInt(r.Generation, 10)
		annotations[requestAnnotation] = hex.EncodeToString(requestDigest[:])
	}
	return objects
}

func (s *Server) reconcile(ctx context.Context, r Request) error {
	objects := s.desired(r)
	// Reject every pre-existing foreign object before making any tenant changes.
	for _, obj := range objects {
		existing := obj.DeepCopyObject().(client.Object)
		err := s.kube.Get(ctx, client.ObjectKeyFromObject(obj), existing)
		if err == nil && stale(existing, obj) {
			return errStale
		}
		if err == nil && (!owned(existing, r.Workspace.ID) || !existing.GetDeletionTimestamp().IsZero()) {
			return errCollision
		}
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, obj := range objects {
		if err := s.apply(ctx, obj, r.Workspace.ID); err != nil {
			return err
		}
	}
	return nil
}
func (s *Server) apply(ctx context.Context, desired client.Object, workspace string) error {
	current := desired.DeepCopyObject().(client.Object)
	_, err := controllerutil.CreateOrUpdate(ctx, s.kube, current, func() error {
		if current.GetResourceVersion() != "" && !owned(current, workspace) {
			return errCollision
		}
		if current.GetResourceVersion() != "" && stale(current, desired) {
			return errStale
		}
		labels := maps.Clone(current.GetLabels())
		if labels == nil {
			labels = map[string]string{}
		}
		maps.Copy(labels, desired.GetLabels())
		current.SetLabels(labels)
		annotations := maps.Clone(current.GetAnnotations())
		if annotations == nil {
			annotations = map[string]string{}
		}
		maps.Copy(annotations, desired.GetAnnotations())
		current.SetAnnotations(annotations)
		switch obj := current.(type) {
		case *api.Organization:
			obj.Spec = desired.(*api.Organization).Spec
		case *api.Agent:
			volume := obj.Spec.Workspace.Volume
			existing := obj.ResourceVersion != ""
			obj.Spec = desired.(*api.Agent).Spec
			if existing {
				obj.Spec.Workspace.Volume = volume
			}
		case *corev1.Secret:
			obj.Data = desired.(*corev1.Secret).Data
		case *corev1.Service:
			spec := desired.(*corev1.Service).Spec
			obj.Spec.Selector = spec.Selector
			obj.Spec.Ports = spec.Ports
		case *networkingv1.NetworkPolicy:
			obj.Spec = desired.(*networkingv1.NetworkPolicy).Spec
		}
		return nil
	})
	return err
}

func stale(current, desired client.Object) bool {
	old, _ := strconv.ParseInt(current.GetAnnotations()[generationAnnotation], 10, 64)
	next, _ := strconv.ParseInt(desired.GetAnnotations()[generationAnnotation], 10, 64)
	return old > next || (old == next && current.GetAnnotations()[requestAnnotation] != desired.GetAnnotations()[requestAnnotation])
}

type AgentState struct {
	Role   string `json:"role"`
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}
type State struct {
	Generation int64        `json:"generation"`
	State      string       `json:"state"`
	Agents     []AgentState `json:"agents"`
}

func (s *Server) state(ctx context.Context, workspace string) (State, error) {
	orgName, pm, eng := names(workspace)
	org := &api.Organization{}
	if err := s.kube.Get(ctx, client.ObjectKey{Name: orgName}, org); err != nil {
		return State{}, err
	}
	if !owned(org, workspace) {
		return State{}, errCollision
	}
	generation, _ := strconv.ParseInt(org.Annotations[generationAnnotation], 10, 64)
	consistent, err := s.consistent(ctx, workspace, org.Annotations)
	if err != nil {
		return State{}, err
	}
	result := State{Generation: generation, State: readyState, Agents: []AgentState{}}
	orgReady := consistent && ready(org.Status.Conditions, org.Generation, org.Status.ObservedGeneration)
	for i, name := range []string{pm, eng} {
		role := "project-manager"
		if i == 1 {
			role = "engineer"
		}
		entry := AgentState{Role: role, Name: name, Reason: "Provisioning"}
		agent := &api.Agent{}
		err := s.kube.Get(ctx, client.ObjectKey{Name: name}, agent)
		if err != nil && !apierrors.IsNotFound(err) {
			return State{}, err
		}
		if err == nil {
			if !owned(agent, workspace) {
				return State{}, errCollision
			}
			entry.Name = agent.Annotations[displayAnnotation]
			workloadReady, err := s.workloadReady(ctx, name)
			if err != nil {
				return State{}, err
			}
			entry.Ready = orgReady && workloadReady && ready(agent.Status.Conditions, agent.Generation, agent.Status.ObservedGeneration)
			if entry.Ready {
				entry.Reason = ""
			} else if condition := apiMeta.FindStatusCondition(agent.Status.Conditions, "Ready"); condition != nil && condition.ObservedGeneration == agent.Generation {
				entry.Reason = condition.Reason
			}
			if condition := apiMeta.FindStatusCondition(agent.Status.Conditions, "Accepted"); condition != nil && condition.ObservedGeneration == agent.Generation && condition.Status == metav1.ConditionFalse {
				result.State = failedState
			}
		}
		if !entry.Ready && result.State != failedState {
			result.State = "provisioning"
		}
		result.Agents = append(result.Agents, entry)
	}
	return result, nil
}
func ready(conditions []metav1.Condition, generation, observed int64) bool {
	condition := apiMeta.FindStatusCondition(conditions, "Ready")
	return observed == generation && condition != nil && condition.ObservedGeneration == generation && condition.Status == metav1.ConditionTrue
}

// A partial or overlapping rollout is never reported ready. Every resource must
// have converged to the Organization fence before exposing the pair as usable.
func (s *Server) consistent(ctx context.Context, workspace string, annotations map[string]string) (bool, error) {
	for _, desired := range s.desired(Request{Workspace: Workspace{ID: workspace}}) {
		obj := desired.DeepCopyObject().(client.Object)
		if err := s.kube.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if !owned(obj, workspace) {
			return false, errCollision
		}
		if obj.GetAnnotations()[generationAnnotation] != annotations[generationAnnotation] || obj.GetAnnotations()[requestAnnotation] != annotations[requestAnnotation] {
			return false, nil
		}
	}
	return true, nil
}

func (s *Server) workloadReady(ctx context.Context, name string) (bool, error) {
	deployment := &appsv1.Deployment{}
	err := s.kube.Get(ctx, client.ObjectKey{Namespace: "agent-" + name, Name: runtimeName}, deployment)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}
	return desired > 0 && deployment.Status.ObservedGeneration == deployment.Generation && deployment.Status.UpdatedReplicas == desired && deployment.Status.Replicas == desired && deployment.Status.AvailableReplicas == desired, nil
}
