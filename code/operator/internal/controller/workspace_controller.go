package controller

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	api "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const workspaceWorkloadValue = "workspace"
const workspaceTokenKey = "token"
const workspaceWorkloadLabel = "aioutfitter.com/workload"
const workspaceGenerationLabel = "aioutfitter.com/compute-generation"

const workspaceFinalizer = "workspaces.aioutfitter.com/cleanup"
const workspaceLabel = "aioutfitter.com/workspace"
const tenantLabel = "aioutfitter.com/tenant"

// The inference relay sidecar is the runtime's only route to inference. It presents a projected
// ServiceAccount token bound to inferenceAudience, mounted into the sidecar only, so the runtime
// container never holds the Pod's inference identity. The runtime reaches it over Pod-shared loopback.
const (
	inferenceRelayName             = "inference"
	inferenceTokenVolumeName       = "inference-token"
	inferenceAudience              = "outfitter-inference"
	inferenceTokenMountPath        = "/var/run/secrets/outfitter/inference"
	inferenceTokenFile             = "token"
	inferenceRelayPort       int32 = 4141
)

// inferenceTokenExpirationSeconds is the projected token lifetime; the kubelet rotates it and the
// relay re-reads the file on every request.
var inferenceTokenExpirationSeconds = ptr.To[int64](3600)

// defaultTerminationGraceSeconds lets a runtime finish its in-flight stage (bounded by the gateway's
// one-hour run limit) and persist state before the kubelet force-kills it.
const defaultTerminationGraceSeconds int64 = 3900

// cleanupGraceSeconds bounds expiry: the volume is about to be deleted, so only a final state write matters.
const cleanupGraceSeconds int64 = 30

const (
	reasonStarting    = "Starting"
	reasonRunning     = "Running"
	reasonStopping    = "Stopping"
	reasonSleeping    = "Sleeping"
	reasonInterrupted = "Interrupted"
)

var digestPinned = regexp.MustCompile(`^[^@\s]+@sha256:[a-f0-9]{64}$`)

// ImageIsDigestPinned reports whether an image reference is immutable (digest-pinned).
func ImageIsDigestPinned(ref string) bool { return digestPinned.MatchString(ref) }

// WorkspaceReconciler deliberately never invokes resident provisioning.
type WorkspaceReconciler struct {
	client.Client
	// APIReader bypasses the informer cache. Pod existence decides interruption and the one-writer
	// guard, so those reads must never lag behind a Pod the controller just created or deleted.
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Image     string
	// RelayImage runs the inference relay sidecar (the webapp image).
	RelayImage       string
	GatewayURL       string
	GatewayNamespace string
	Model            string
	// TerminationGracePeriodSeconds is given to every runtime Pod; zero selects the default.
	TerminationGracePeriodSeconds int64
	Now                           func() time.Time
}

func (r *WorkspaceReconciler) grace() int64 {
	if r.TerminationGracePeriodSeconds > 0 {
		return r.TerminationGracePeriodSeconds
	}
	return defaultTerminationGraceSeconds
}

// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=services;persistentvolumeclaims;secrets;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages a temporary workspace and its bounded lifetime.
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	w := &api.Workspace{}
	// Uncached read: status.podName is cleared on the server right before the controller stops a Pod,
	// and the Pod can exit before the Workspace informer delivers that write. Judging a fresh Pod list
	// against a stale podName would turn a normal stop into a false, sticky Interrupted.
	if err := r.reader().Get(ctx, req.NamespacedName, w); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !w.DeletionTimestamp.IsZero() {
		return r.cleanup(ctx, w)
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now()
	}
	if !controllerutil.ContainsFinalizer(w, workspaceFinalizer) {
		base := w.DeepCopy()
		controllerutil.AddFinalizer(w, workspaceFinalizer)
		if err := r.Patch(ctx, w, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	observed := w.DeepCopy()
	if !now.Before(w.Spec.ExpiresAt.Time) {
		// A concurrent wake/renewal must win over a stale expiry decision.
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, w, client.Preconditions{UID: &w.UID, ResourceVersion: &w.ResourceVersion}))
	}
	until := w.Spec.ExpiresAt.Sub(now)
	// An interrupted environment is terminal: no recreation, no replay, no wake. Only expiry ends it.
	if ready := meta.FindStatusCondition(w.Status.Conditions, "Ready"); ready != nil && ready.Reason == reasonInterrupted {
		return r.condition(ctx, observed, w, false, reasonInterrupted, ready.Message, until)
	}
	if w.Spec.ComputeGeneration < 1 {
		return r.condition(ctx, observed, w, false, "InvalidSpec", "computeGeneration is required", time.Minute)
	}
	awake := now.Before(w.Spec.AwakeUntil.Time)
	if awake && w.Spec.AwakeUntil.Sub(now) < until {
		until = w.Spec.AwakeUntil.Sub(now)
	}
	pods, err := r.workspacePods(ctx, w)
	if err != nil {
		return ctrl.Result{}, err
	}
	// The recorded Pod is classified before any sleep or supersession can hide what happened to it.
	if reason, message := classify(w, pods); reason != "" {
		return r.condition(ctx, observed, w, false, reason, message, min(until, 5*time.Second))
	}
	// Idle shutdown must not depend on credentials or provisioning succeeding.
	if !awake {
		if res, err := r.suspend(ctx, observed, w, pods); res != nil || err != nil {
			return ptr.Deref(res, ctrl.Result{}), err
		}
		if len(pods) > 0 {
			return r.condition(ctx, observed, w, false, reasonStopping, "Stopping runtime for sleep; files and session retained", 2*time.Second)
		}
		return r.condition(ctx, observed, w, false, reasonSleeping, "Files and session retained; runtime suspended", until)
	}
	if res, err := r.admit(ctx, observed, w); res != nil || err != nil {
		return ptr.Deref(res, ctrl.Result{}), err
	}
	if err := r.resources(ctx, w); err != nil {
		return ctrl.Result{}, err
	}
	return r.compute(ctx, observed, w, pods, until)
}

// classify inspects the Pod recorded in status. The controller clears status.podName before it stops a
// Pod itself, so a recorded Pod that is terminating, exited or missing was lost to something else:
// a draining one is Stopping (accepted work may still finish and be read), anything else is Interrupted.
func classify(w *api.Workspace, pods []corev1.Pod) (string, string) {
	name := w.Status.PodName
	if name == "" {
		return "", ""
	}
	for i := range pods {
		pod := &pods[i]
		if pod.Name != name {
			continue
		}
		switch {
		case pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded:
			return reasonInterrupted, "Workspace runtime Pod " + name + " exited (" + string(pod.Status.Phase) + "); environment interrupted"
		case pod.DeletionTimestamp != nil:
			return reasonStopping, "Workspace runtime Pod " + name + " is being terminated externally; accepted work may finish, no new work or wake"
		}
		return "", ""
	}
	return reasonInterrupted, "Workspace runtime Pod " + name + " disappeared; environment interrupted"
}

// admit checks the tenant namespace, controller configuration and scoped credential. A non-nil result
// means the workspace cannot be provisioned yet and carries the condition to report.
func (r *WorkspaceReconciler) admit(ctx context.Context, observed, w *api.Workspace) (*ctrl.Result, error) {
	report := func(reason, message string, after time.Duration) (*ctrl.Result, error) {
		res, err := r.condition(ctx, observed, w, false, reason, message, after)
		return &res, err
	}
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: w.Namespace}, ns); err != nil {
		return nil, err
	}
	if ns.Labels[tenantLabel] == "" {
		return report("InvalidNamespace", "Workspace requires a platform-managed tenant namespace", time.Minute)
	}
	if r.Image == "" || r.RelayImage == "" || r.GatewayURL == "" || r.GatewayNamespace == "" {
		return report("NotConfigured", "Workspace runtime is not configured", time.Minute)
	}
	if err := r.policies(ctx, w.Namespace); err != nil {
		return nil, err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: w.Spec.CredentialSecretName, Namespace: w.Namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return report("CredentialsMissing", "Waiting for workspace credential", 5*time.Second)
		}
		return nil, err
	}
	if secret.Labels[workspaceLabel] != w.Name || len(secret.Data[workspaceTokenKey]) < 32 {
		return report("InvalidCredential", "Credential must belong to this workspace", time.Minute)
	}
	// Secrets come from the gateway. Adopt only the explicitly labeled per-workspace secret.
	if !metav1.IsControlledBy(secret, w) {
		if metav1.GetControllerOf(secret) != nil {
			return nil, fmt.Errorf("workspace credential already has an owner")
		}
		base := secret.DeepCopy()
		if err := controllerutil.SetControllerReference(w, secret, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Patch(ctx, secret, client.MergeFrom(base)); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// compute drives exactly one runtime Pod per compute generation. The image is captured at creation and
// an existing Pod is never rewritten.
func (r *WorkspaceReconciler) compute(ctx context.Context, observed, w *api.Workspace, pods []corev1.Pod, until time.Duration) (ctrl.Result, error) {
	generation := w.Spec.ComputeGeneration
	name := workspacePodName(w, generation)
	var current *corev1.Pod
	var others []corev1.Pod
	for i := range pods {
		if pods[i].Name == name {
			current = &pods[i]
		} else {
			others = append(others, pods[i])
		}
	}
	if current == nil {
		if w.Status.ComputeGeneration == generation {
			// classify already ruled out a lost Pod: the controller stopped this generation itself.
			return r.condition(ctx, observed, w, false, reasonSleeping, "Runtime stopped; a new compute generation is required to wake", until)
		}
		// One writer per volume: a superseded generation must be gone before the next starts.
		if len(others) > 0 {
			if res, err := r.forget(ctx, observed, w); res != nil || err != nil {
				return ptr.Deref(res, ctrl.Result{}), err
			}
			if err := r.deleteOwnedPods(ctx, w, others); err != nil {
				return ctrl.Result{}, err
			}
			return r.condition(ctx, observed, w, false, reasonStarting, "Waiting for previous runtime Pod to stop", 2*time.Second)
		}
		return r.start(ctx, observed, w, generation, name, until)
	}
	if !metav1.IsControlledBy(current, w) {
		return ctrl.Result{}, fmt.Errorf("refusing to adopt existing Pod %s", current.Name)
	}
	if current.DeletionTimestamp != nil {
		// The controller itself stopped this Pod; a late renewal cannot reuse the generation.
		return r.condition(ctx, observed, w, false, reasonStopping, "Runtime stopping; a new compute generation is required to wake", 2*time.Second)
	}
	w.Status.ComputeGeneration, w.Status.PodName, w.Status.ResolvedImage = generation, name, current.Spec.Containers[0].Image
	switch current.Status.Phase {
	case corev1.PodFailed, corev1.PodSucceeded:
		return r.condition(ctx, observed, w, false, reasonInterrupted, "Workspace runtime Pod "+name+" exited ("+string(current.Status.Phase)+"); environment interrupted", until)
	case corev1.PodRunning:
		for _, c := range current.Status.Conditions {
			if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
				return r.condition(ctx, observed, w, true, reasonRunning, "Workspace runtime is ready", until)
			}
		}
	}
	return r.condition(ctx, observed, w, false, reasonStarting, "Waiting for workspace runtime", min(until, 5*time.Second))
}

// start creates the Pod for a new generation. Starting is recorded before the create so a pending
// generation is never mistaken for Sleeping; the Pod is recorded only once it exists.
func (r *WorkspaceReconciler) start(ctx context.Context, observed, w *api.Workspace, generation int64, name string, until time.Duration) (ctrl.Result, error) {
	pending := fmt.Sprintf("Starting compute generation %d", generation)
	if ready := meta.FindStatusCondition(w.Status.Conditions, "Ready"); ready == nil || ready.Reason != reasonStarting || ready.Message != pending {
		if _, err := r.condition(ctx, observed, w, false, reasonStarting, pending, time.Second); err != nil {
			return ctrl.Result{}, err
		}
		observed = w.DeepCopy()
	}
	image := w.Spec.Image
	if image == "" {
		image = r.Image
	}
	pod := r.runtimePod(w, generation, image)
	if err := controllerutil.SetControllerReference(w, pod, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Create(ctx, pod); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			_, _ = r.condition(ctx, observed, w, false, reasonStarting, pending+": "+err.Error(), time.Second)
			return ctrl.Result{}, err
		}
		if err := r.reader().Get(ctx, client.ObjectKeyFromObject(pod), pod); err != nil {
			return ctrl.Result{}, err
		}
		if !metav1.IsControlledBy(pod, w) {
			return ctrl.Result{}, fmt.Errorf("refusing to adopt existing Pod %s", pod.Name)
		}
		image = pod.Spec.Containers[0].Image
	}
	w.Status.ComputeGeneration, w.Status.PodName, w.Status.ResolvedImage = generation, name, image
	return r.condition(ctx, observed, w, false, reasonStarting, "Waiting for workspace runtime", min(until, 5*time.Second))
}

func workspacePodName(w *api.Workspace, generation int64) string {
	return fmt.Sprintf("%s-g%d", w.Name, generation)
}

// reader is the uncached reader when configured, otherwise the plain client (tests use a direct client).
func (r *WorkspaceReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// workspacePods lists this workspace's Pods without the informer cache.
func (r *WorkspaceReconciler) workspacePods(ctx context.Context, w *api.Workspace) ([]corev1.Pod, error) {
	pods := &corev1.PodList{}
	if err := r.reader().List(ctx, pods, client.InNamespace(w.Namespace), client.MatchingLabels{workspaceLabel: w.Name}); err != nil {
		return nil, err
	}
	return pods.Items, nil
}

// deleteOwnedPods stops this workspace's own runtime Pods with their full grace.
func (r *WorkspaceReconciler) deleteOwnedPods(ctx context.Context, w *api.Workspace, pods []corev1.Pod) error {
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil || !metav1.IsControlledBy(pod, w) {
			continue
		}
		if err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID}); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// forget clears status.podName before the controller stops a Pod, so that stop is never read as a
// failure. The patch is serialized against concurrent renewals: on conflict the decision is retaken.
func (r *WorkspaceReconciler) forget(ctx context.Context, observed, w *api.Workspace) (*ctrl.Result, error) {
	if w.Status.PodName == "" {
		return nil, nil
	}
	base := w.DeepCopy()
	w.Status.PodName = ""
	if err := r.Status().Patch(ctx, w, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		if apierrors.IsConflict(err) {
			return &ctrl.Result{Requeue: true}, nil
		}
		return nil, err
	}
	*observed = *w.DeepCopy()
	return nil, nil
}

// suspend shuts down compute even if provisioning dependencies are unavailable.
func (r *WorkspaceReconciler) suspend(ctx context.Context, observed, w *api.Workspace, pods []corev1.Pod) (*ctrl.Result, error) {
	if res, err := r.forget(ctx, observed, w); res != nil || err != nil {
		return res, err
	}
	return nil, r.deleteOwnedPods(ctx, w, pods)
}

// condition records the Ready state together with any status fields changed since observed was read.
// Interrupted and Stopping are judged from the Workspace as read; a concurrent spec or status write
// makes that judgement stale, so those two are written under an optimistic lock and retaken on conflict.
func (r *WorkspaceReconciler) condition(ctx context.Context, observed, w *api.Workspace, ready bool, reason, message string, after time.Duration) (ctrl.Result, error) {
	base := observed.DeepCopy()
	w.Status.ObservedGeneration = w.Generation
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&w.Status.Conditions, metav1.Condition{Type: "Ready", Status: status, Reason: reason, Message: message, ObservedGeneration: w.Generation})
	if !apiequality.Semantic.DeepEqual(base.Status, w.Status) {
		patch := client.MergeFrom(base)
		if reason == reasonInterrupted || reason == reasonStopping {
			patch = client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})
		}
		if err := r.Status().Patch(ctx, w, patch); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{Requeue: true}, nil
			}
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: max(after, time.Second)}, nil
}

func (r *WorkspaceReconciler) owned(ctx context.Context, w *api.Workspace, obj client.Object, mutate func() error) error {
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		if obj.GetResourceVersion() != "" && !metav1.IsControlledBy(obj, w) {
			return fmt.Errorf("refusing to adopt existing %T %s", obj, obj.GetName())
		}
		obj.SetLabels(map[string]string{workspaceLabel: w.Name, workspaceWorkloadLabel: workspaceWorkloadValue})
		if err := controllerutil.SetControllerReference(w, obj, r.Scheme); err != nil {
			return err
		}
		return mutate()
	})
	return err
}

// resources maintains the storage and addressing a workspace owns across every compute generation.
func (r *WorkspaceReconciler) resources(ctx context.Context, w *api.Workspace) error {
	metadata := metav1.ObjectMeta{Name: w.Name, Namespace: w.Namespace}
	sa := &corev1.ServiceAccount{ObjectMeta: metadata}
	if err := r.owned(ctx, w, sa, func() error { sa.AutomountServiceAccountToken = ptr.To(false); return nil }); err != nil {
		return err
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metadata}
	if err := r.owned(ctx, w, pvc, func() error {
		if pvc.ResourceVersion == "" {
			size := w.Spec.StorageSize
			if size == "" {
				size = "5Gi"
			}
			q, err := resource.ParseQuantity(size)
			if err != nil {
				return err
			}
			pvc.Spec = corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: w.Spec.StorageClassName, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: q}}}
		}
		return nil
	}); err != nil {
		return err
	}
	svc := &corev1.Service{ObjectMeta: metadata}
	return r.owned(ctx, w, svc, func() error {
		svc.Spec.Selector = map[string]string{workspaceLabel: w.Name}
		// A draining Pod must stay addressable so an accepted run can finish and be read (Stopping).
		svc.Spec.PublishNotReadyAddresses = true
		svc.Spec.Ports = []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080)}}
		return nil
	})
}

// runtimePod is the direct runtime Pod for one compute generation. It never restarts: any exit interrupts
// the environment instead of replaying agent work. The runtime container stays first: status reads its image.
func (r *WorkspaceReconciler) runtimePod(w *api.Workspace, generation int64, image string) *corev1.Pod {
	gen := strconv.FormatInt(generation, 10)
	containerSecurity := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}
	relayURL := "http://127.0.0.1:" + strconv.Itoa(int(inferenceRelayPort))
	labels := map[string]string{workspaceLabel: w.Name, workspaceWorkloadLabel: workspaceWorkloadValue, workspaceGenerationLabel: gen}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: workspacePodName(w, generation), Namespace: w.Namespace, Labels: labels}, Spec: corev1.PodSpec{
		ServiceAccountName: w.Name,
		RestartPolicy:      corev1.RestartPolicyNever, DNSPolicy: corev1.DNSClusterFirst,
		AutomountServiceAccountToken: ptr.To(false), TerminationGracePeriodSeconds: ptr.To(r.grace()),
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
		Containers: []corev1.Container{{Name: "runtime", Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
			SecurityContext: containerSecurity,
			Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
			Env:             []corev1.EnvVar{{Name: "HOME", Value: "/workspace"}, {Name: "WORKSPACE_ID", Value: w.Name}, {Name: "WORKSPACE_COMPUTE_GENERATION", Value: gen}, {Name: "INFERENCE_BASE_URL", Value: relayURL + "/v1"}, {Name: "AI_MODEL", Value: r.Model}, {Name: "WORKSPACE_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: w.Spec.CredentialSecretName}, Key: workspaceTokenKey}}}},
			Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi"), corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")}},
			VolumeMounts:    []corev1.VolumeMount{{Name: workspaceWorkloadValue, MountPath: "/workspace"}, {Name: "tmp", MountPath: "/tmp"}},
			ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(8080), Scheme: corev1.URISchemeHTTP}}, PeriodSeconds: 3, TimeoutSeconds: 2, SuccessThreshold: 1, FailureThreshold: 3},
		}, {Name: inferenceRelayName, Image: r.RelayImage, ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"node", "workspace/inference-relay.mjs"},
			SecurityContext: containerSecurity,
			Env:             []corev1.EnvVar{{Name: "INFERENCE_GATEWAY_URL", Value: r.GatewayURL}, {Name: "INFERENCE_TOKEN_FILE", Value: inferenceTokenMountPath + "/" + inferenceTokenFile}, {Name: "PORT", Value: strconv.Itoa(int(inferenceRelayPort))}},
			Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}},
			VolumeMounts:    []corev1.VolumeMount{{Name: inferenceTokenVolumeName, MountPath: inferenceTokenMountPath, ReadOnly: true}},
			ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(inferenceRelayPort), Scheme: corev1.URISchemeHTTP}}, PeriodSeconds: 3, TimeoutSeconds: 2, SuccessThreshold: 1, FailureThreshold: 3},
		}},
		Volumes: []corev1.Volume{{Name: workspaceWorkloadValue, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: w.Name}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("256Mi"))}}},
			{Name: inferenceTokenVolumeName, VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Audience: inferenceAudience, ExpirationSeconds: inferenceTokenExpirationSeconds, Path: inferenceTokenFile}}}}}}},
	}}
}

func (r *WorkspaceReconciler) policies(ctx context.Context, namespace string) error {
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "workspace-boundary", Namespace: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		policy.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{workspaceWorkloadLabel: workspaceWorkloadValue}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Ingress:     []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: r.GatewayNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "outfitter-webapp"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(8080))}}}},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolUDP), Port: ptr.To(intstr.FromInt32(53))}, {Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(53))}}},
				{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: r.GatewayNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "outfitter-webapp"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: ptr.To(corev1.ProtocolTCP), Port: ptr.To(intstr.FromInt32(4040))}}},
			},
		}
		return nil
	})
	return err
}

func (r *WorkspaceReconciler) cleanup(ctx context.Context, w *api.Workspace) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(w, workspaceFinalizer) {
		return ctrl.Result{}, nil
	}
	pods, err := r.workspacePods(ctx, w)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(pods) > 0 {
		// Every runtime Pod, including a retained failed one or one still draining with the long sleep
		// grace, is removed before the volume. The volume is going away, so expiry only waits for a final
		// state write: a repeated delete with a shorter grace shortens an in-progress termination.
		for i := range pods {
			pod := &pods[i]
			if pod.DeletionTimestamp != nil && pod.DeletionGracePeriodSeconds != nil && *pod.DeletionGracePeriodSeconds <= cleanupGraceSeconds {
				continue
			}
			if !metav1.IsControlledBy(pod, w) {
				return ctrl.Result{}, fmt.Errorf("refusing to delete unowned Pod %s", pod.Name)
			}
			if err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID}, client.GracePeriodSeconds(cleanupGraceSeconds)); client.IgnoreNotFound(err) != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	for _, obj := range []client.Object{&corev1.PersistentVolumeClaim{}, &corev1.Service{}, &corev1.ServiceAccount{}, &corev1.Secret{}} {
		name := w.Name
		if _, ok := obj.(*corev1.Secret); ok {
			name = w.Spec.CredentialSecretName
		}
		err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: w.Namespace}, obj)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return ctrl.Result{}, err
		}
		if !metav1.IsControlledBy(obj, w) {
			// The gateway creates the scoped Secret before the controller can adopt it.
			// An expired or partially provisioned workspace must also clean that Secret.
			_, isSecret := obj.(*corev1.Secret)
			if !isSecret || metav1.GetControllerOf(obj) != nil || obj.GetLabels()[workspaceLabel] != w.Name {
				return ctrl.Result{}, fmt.Errorf("refusing to delete unowned %T", obj)
			}
		}
		uid, version := obj.GetUID(), obj.GetResourceVersion()
		if err = r.Delete(ctx, obj, client.Preconditions{UID: &uid, ResourceVersion: &version}); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	base := w.DeepCopy()
	controllerutil.RemoveFinalizer(w, workspaceFinalizer)
	return ctrl.Result{}, r.Patch(ctx, w, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&api.Workspace{}).Owns(&corev1.Pod{}).Owns(&corev1.PersistentVolumeClaim{}).Owns(&corev1.Secret{}).Named(workspaceWorkloadValue).Complete(r)
}
