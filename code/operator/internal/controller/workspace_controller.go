package controller

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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

const workspaceFinalizer = "workspaces.aioutfitter.com/cleanup"
const workspaceLabel = "aioutfitter.com/workspace"
const tenantLabel = "aioutfitter.com/tenant"

// WorkspaceReconciler deliberately never invokes resident provisioning.
type WorkspaceReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Image            string
	GatewayURL       string
	GatewayNamespace string
	Model            string
	Now              func() time.Time
}

// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aioutfitter.com,resources=workspaces/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services;persistentvolumeclaims;secrets;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages a temporary workspace and its bounded lifetime.
func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	w := &api.Workspace{}
	if err := r.Get(ctx, req.NamespacedName, w); err != nil {
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
	if !now.Before(w.Spec.ExpiresAt.Time) {
		// A concurrent wake/renewal must win over a stale expiry decision.
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, w, client.Preconditions{UID: &w.UID, ResourceVersion: &w.ResourceVersion}))
	}
	awake := now.Before(w.Spec.AwakeUntil.Time)
	// Idle shutdown must not depend on credentials or provisioning succeeding.
	if !awake {
		if err := r.suspend(ctx, w); err != nil {
			return ctrl.Result{}, err
		}
	}
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: w.Namespace}, ns); err != nil {
		return ctrl.Result{}, err
	}
	if ns.Labels[tenantLabel] == "" {
		return r.condition(ctx, w, false, "InvalidNamespace", "Workspace requires a platform-managed tenant namespace", time.Minute)
	}
	if r.Image == "" || r.GatewayURL == "" || r.GatewayNamespace == "" {
		return r.condition(ctx, w, false, "NotConfigured", "Workspace runtime is not configured", time.Minute)
	}
	if err := r.policies(ctx, w.Namespace); err != nil {
		return ctrl.Result{}, err
	}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: w.Spec.CredentialSecretName, Namespace: w.Namespace}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return r.condition(ctx, w, false, "CredentialsMissing", "Waiting for workspace credential", 5*time.Second)
		}
		return ctrl.Result{}, err
	}
	if secret.Labels[workspaceLabel] != w.Name || len(secret.Data[workspaceTokenKey]) < 32 {
		return r.condition(ctx, w, false, "InvalidCredential", "Credential must belong to this workspace", time.Minute)
	}
	// Secrets come from the gateway. Adopt only the explicitly labeled per-workspace secret.
	if !metav1.IsControlledBy(secret, w) {
		if metav1.GetControllerOf(secret) != nil {
			return ctrl.Result{}, fmt.Errorf("workspace credential already has an owner")
		}
		base := secret.DeepCopy()
		if err := controllerutil.SetControllerReference(w, secret, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Patch(ctx, secret, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}
	deployment, err := r.resources(ctx, w, awake)
	if err != nil {
		return ctrl.Result{}, err
	}
	until := w.Spec.ExpiresAt.Sub(now)
	if awake && w.Spec.AwakeUntil.Sub(now) < until {
		until = w.Spec.AwakeUntil.Sub(now)
	}
	if !awake {
		return r.condition(ctx, w, false, "Sleeping", "Files and session retained; runtime suspended", until)
	}
	if deployment.Status.ObservedGeneration < deployment.Generation || deployment.Status.AvailableReplicas < 1 {
		return r.condition(ctx, w, false, "Starting", "Waiting for workspace runtime", min(until, 5*time.Second))
	}
	return r.condition(ctx, w, true, "Running", "Workspace runtime is ready", until)
}

// suspend shuts down existing compute even if provisioning dependencies are unavailable.
func (r *WorkspaceReconciler) suspend(ctx context.Context, w *api.Workspace) error {
	dep := &appsv1.Deployment{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(w), dep); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(dep, w) {
		return fmt.Errorf("refusing to suspend unowned deployment")
	}
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
		return nil
	}
	base := dep.DeepCopy()
	dep.Spec.Replicas = ptr.To[int32](0)
	return r.Patch(ctx, dep, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

func (r *WorkspaceReconciler) condition(ctx context.Context, w *api.Workspace, ready bool, reason, message string, after time.Duration) (ctrl.Result, error) {
	base := w.DeepCopy()
	w.Status.ObservedGeneration = w.Generation
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	meta.SetStatusCondition(&w.Status.Conditions, metav1.Condition{Type: "Ready", Status: status, Reason: reason, Message: message, ObservedGeneration: w.Generation})
	if !apiequality.Semantic.DeepEqual(base.Status, w.Status) {
		if err := r.Status().Patch(ctx, w, client.MergeFrom(base)); err != nil {
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

func (r *WorkspaceReconciler) resources(ctx context.Context, w *api.Workspace, awake bool) (*appsv1.Deployment, error) {
	metadata := metav1.ObjectMeta{Name: w.Name, Namespace: w.Namespace}
	sa := &corev1.ServiceAccount{ObjectMeta: metadata}
	if err := r.owned(ctx, w, sa, func() error { sa.AutomountServiceAccountToken = ptr.To(false); return nil }); err != nil {
		return nil, err
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
		return nil, err
	}
	svc := &corev1.Service{ObjectMeta: metadata}
	if err := r.owned(ctx, w, svc, func() error {
		svc.Spec.Selector = map[string]string{workspaceLabel: w.Name}
		svc.Spec.Ports = []corev1.ServicePort{{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt32(8080)}}
		return nil
	}); err != nil {
		return nil, err
	}
	dep := &appsv1.Deployment{ObjectMeta: metadata}
	err := r.owned(ctx, w, dep, func() error {
		replicas := int32(0)
		if awake {
			replicas = 1
		}
		dep.Spec.Replicas = &replicas
		dep.Spec.RevisionHistoryLimit = ptr.To[int32](1)
		dep.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
		labels := map[string]string{workspaceLabel: w.Name, workspaceWorkloadLabel: workspaceWorkloadValue}
		dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{workspaceLabel: w.Name}}
		dep.Spec.Template = corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{
			ServiceAccountName: w.Name, DeprecatedServiceAccount: w.Name,
			RestartPolicy: corev1.RestartPolicyAlways, DNSPolicy: corev1.DNSClusterFirst, SchedulerName: corev1.DefaultSchedulerName,
			AutomountServiceAccountToken: ptr.To(false), TerminationGracePeriodSeconds: ptr.To[int64](20),
			SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}},
			Containers: []corev1.Container{{Name: "runtime", Image: r.Image, ImagePullPolicy: corev1.PullIfNotPresent,
				TerminationMessagePath: corev1.TerminationMessagePathDefault, TerminationMessagePolicy: corev1.TerminationMessageReadFile,
				SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
				Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: corev1.ProtocolTCP}},
				Env:             []corev1.EnvVar{{Name: "HOME", Value: "/workspace"}, {Name: "WORKSPACE_ID", Value: w.Name}, {Name: "INFERENCE_BASE_URL", Value: r.GatewayURL + "/inference/" + w.Name + "/v1"}, {Name: "AI_MODEL", Value: r.Model}, {Name: "WORKSPACE_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: w.Spec.CredentialSecretName}, Key: workspaceTokenKey}}}},
				Resources:       corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("200m"), corev1.ResourceMemory: resource.MustParse("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("2Gi"), corev1.ResourceEphemeralStorage: resource.MustParse("1Gi")}},
				VolumeMounts:    []corev1.VolumeMount{{Name: workspaceWorkloadValue, MountPath: "/workspace"}, {Name: "tmp", MountPath: "/tmp"}},
				ReadinessProbe:  &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromInt32(8080), Scheme: corev1.URISchemeHTTP}}, PeriodSeconds: 3, TimeoutSeconds: 2, SuccessThreshold: 1, FailureThreshold: 3},
			}},
			Volumes: []corev1.Volume{{Name: workspaceWorkloadValue, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: w.Name}}}, {Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: ptr.To(resource.MustParse("256Mi"))}}}},
		}}
		return nil
	})
	return dep, err
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
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: w.Name, Namespace: w.Namespace}}
	if err := r.Get(ctx, client.ObjectKeyFromObject(dep), dep); err == nil {
		if !metav1.IsControlledBy(dep, w) {
			return ctrl.Result{}, fmt.Errorf("refusing to delete unowned deployment")
		}
		if err = r.Delete(ctx, dep); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	} else if !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(w.Namespace), client.MatchingLabels{workspaceLabel: w.Name}); err != nil {
		return ctrl.Result{}, err
	}
	if len(pods.Items) > 0 {
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
	return ctrl.NewControllerManagedBy(mgr).For(&api.Workspace{}).Owns(&appsv1.Deployment{}).Owns(&corev1.PersistentVolumeClaim{}).Owns(&corev1.Secret{}).Named(workspaceWorkloadValue).Complete(r)
}
