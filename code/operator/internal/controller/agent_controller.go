package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	linkv1alpha1 "github.com/ncrmro/link-operator/code/operator/api/v1alpha1"
)

const (
	credentialPollInterval = 10 * time.Second
	steadyStateInterval    = 5 * time.Minute
	credentialKindSecret   = "Secret"
	credentialKindConfig   = "ConfigMap"
)

var agentConditionOrder = []string{
	linkv1alpha1.AgentConditionAccepted,
	linkv1alpha1.AgentConditionNamespaceReady,
	linkv1alpha1.AgentConditionWorkspaceReady,
	linkv1alpha1.AgentConditionCredentialsReady,
	linkv1alpha1.AgentConditionOutfitterSettingsReady,
	linkv1alpha1.AgentConditionWorkloadReady,
	linkv1alpha1.AgentConditionReady,
}

// AgentReconciler reconciles an Agent object.
type AgentReconciler struct {
	client.Client
	APIReader         client.Reader
	Scheme            *runtime.Scheme
	AgentImage        string
	OutfitterRevision string
}

// +kubebuilder:rbac:groups=link.aioutfitter.com,resources=agents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=link.aioutfitter.com,resources=agents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=link.aioutfitter.com,resources=agents/finalizers,verbs=update
// +kubebuilder:rbac:groups=link.aioutfitter.com,resources=organizations,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts;resourcequotas;limitranges;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;create;update;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,resourceNames=admin,verbs=bind
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile materializes the agent's namespace workspace and runtime.
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	agent := &linkv1alpha1.Agent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !agent.DeletionTimestamp.IsZero() {
		return r.finalizeAgent(ctx, agent)
	}
	if !controllerutil.ContainsFinalizer(agent, linkv1alpha1.AgentFinalizer) {
		base := agent.DeepCopy()
		controllerutil.AddFinalizer(agent, linkv1alpha1.AgentFinalizer)
		if err := r.Patch(ctx, agent, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	statusBase := agent.DeepCopy()
	agent.Status.ObservedGeneration = agent.Generation
	agent.Status.Namespace = agentNamespace(agent.Name)
	agent.Status.OutfitterRevision = r.OutfitterRevision

	organization, validationMessage, err := r.validateAgent(ctx, agent)
	if err != nil {
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	if validationMessage != "" {
		setAgentCondition(agent, linkv1alpha1.AgentConditionAccepted, metav1.ConditionFalse, "InvalidSpecification", validationMessage)
		blockAgentConditions(agent, linkv1alpha1.AgentConditionNamespaceReady, "NotAccepted", "Agent is not accepted")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, nil)
	}
	setAgentCondition(agent, linkv1alpha1.AgentConditionAccepted, metav1.ConditionTrue, "Accepted", "Agent specification and membership are valid")

	if err := r.ensureAgentNamespace(ctx, agent); err != nil {
		setAgentCondition(agent, linkv1alpha1.AgentConditionNamespaceReady, metav1.ConditionFalse, "NamespaceReconcileFailed", "Agent namespace could not be reconciled")
		blockAgentConditions(agent, linkv1alpha1.AgentConditionWorkspaceReady, "NamespaceNotReady", "Agent namespace is not ready")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	setAgentCondition(agent, linkv1alpha1.AgentConditionNamespaceReady, metav1.ConditionTrue, "Ready", "Agent namespace is ready")

	quota, err := r.ensureWorkspaceResources(ctx, agent)
	if err != nil {
		setAgentCondition(agent, linkv1alpha1.AgentConditionWorkspaceReady, metav1.ConditionFalse, "WorkspaceReconcileFailed", "Agent workspace guardrails could not be reconciled")
		blockAgentConditions(agent, linkv1alpha1.AgentConditionCredentialsReady, "WorkspaceNotReady", "Agent workspace is not ready")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	agent.Status.QuotaHard = quota.Status.Hard.DeepCopy()
	agent.Status.QuotaUsed = quota.Status.Used.DeepCopy()
	if len(agent.Status.QuotaHard) == 0 {
		agent.Status.QuotaHard = quota.Spec.Hard.DeepCopy()
	}
	setAgentCondition(agent, linkv1alpha1.AgentConditionWorkspaceReady, metav1.ConditionTrue, "Ready", "Agent workspace guardrails are ready")

	missingCredentials, err := r.missingCredentialReferences(ctx, agent)
	if err != nil {
		setAgentCondition(agent, linkv1alpha1.AgentConditionCredentialsReady, metav1.ConditionFalse, "CredentialCheckFailed", "Referenced objects could not be checked")
		blockAgentConditions(agent, linkv1alpha1.AgentConditionOutfitterSettingsReady, "CredentialsUnknown", "Credential readiness is unknown")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	if len(missingCredentials) > 0 {
		setAgentCondition(agent, linkv1alpha1.AgentConditionCredentialsReady, metav1.ConditionFalse, "ObjectsMissing", "Missing referenced objects: "+strings.Join(missingCredentials, ", "))
	} else {
		setAgentCondition(agent, linkv1alpha1.AgentConditionCredentialsReady, metav1.ConditionTrue, "Ready", "All referenced objects exist")
	}

	if err := r.ensureOutfitterSettings(ctx, agent, organization); err != nil {
		setAgentCondition(agent, linkv1alpha1.AgentConditionOutfitterSettingsReady, metav1.ConditionFalse, "SettingsReconcileFailed", "Outfitter settings could not be reconciled")
		blockAgentConditions(agent, linkv1alpha1.AgentConditionWorkloadReady, "SettingsNotReady", "Outfitter settings are not ready")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	catalogSource := organization.Spec.AgentCatalogs[0]
	revision := ""
	if catalogSource.Revision != nil {
		revision = strings.ToLower(*catalogSource.Revision)
	}
	agent.Status.CatalogSources = []linkv1alpha1.CatalogSourceStatus{{
		Name:     catalogSource.Name,
		Revision: revision,
	}}
	setAgentCondition(agent, linkv1alpha1.AgentConditionOutfitterSettingsReady, metav1.ConditionTrue, "Ready", "Outfitter settings contain the pinned source; runtime resolution is delegated to Outfitter")

	deployment, err := r.ensureAgentDeployment(ctx, agent, organization)
	if err != nil {
		setAgentCondition(agent, linkv1alpha1.AgentConditionWorkloadReady, metav1.ConditionFalse, "WorkloadReconcileFailed", "Agent Deployment could not be reconciled")
		setAgentCondition(agent, linkv1alpha1.AgentConditionReady, metav1.ConditionFalse, "WorkloadNotReady", "Agent workload is not ready")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{}, err)
	}
	if deployment.Status.AvailableReplicas < 1 {
		setAgentCondition(agent, linkv1alpha1.AgentConditionWorkloadReady, metav1.ConditionFalse, "DeploymentUnavailable", "Agent Deployment has no available replica")
		setAgentCondition(agent, linkv1alpha1.AgentConditionReady, metav1.ConditionFalse, "WorkloadNotReady", "Agent workload is not ready")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{RequeueAfter: credentialPollInterval}, nil)
	}
	setAgentCondition(agent, linkv1alpha1.AgentConditionWorkloadReady, metav1.ConditionTrue, "Ready", "Agent Deployment is available")
	if len(missingCredentials) > 0 {
		setAgentCondition(agent, linkv1alpha1.AgentConditionReady, metav1.ConditionFalse, "CredentialsNotReady", "Agent references missing objects")
		return r.finishAgent(ctx, statusBase, agent, ctrl.Result{RequeueAfter: credentialPollInterval}, nil)
	}
	setAgentCondition(agent, linkv1alpha1.AgentConditionReady, metav1.ConditionTrue, "Ready", "Agent is ready")

	return r.finishAgent(ctx, statusBase, agent, ctrl.Result{RequeueAfter: steadyStateInterval}, nil)
}

func (r *AgentReconciler) validateAgent(
	ctx context.Context,
	agent *linkv1alpha1.Agent,
) (*linkv1alpha1.Organization, string, error) {
	if len(agent.Name) > 57 {
		return nil, "Agent name must be no longer than 57 characters", nil
	}
	if len(agent.Spec.Memberships) != 1 {
		return nil, "M1 agents must declare exactly one organization membership", nil
	}
	missingQuotaKeys := requiredQuotaKeysMissing(agent.Spec.Workspace.ResourceQuota.Hard)
	if len(missingQuotaKeys) > 0 {
		return nil, "Resource quota is missing required keys: " + strings.Join(missingQuotaKeys, ", "), nil
	}
	if message := storageQuotaValidationMessage(agent); message != "" {
		return nil, message, nil
	}
	if !hasComputeDefaults(agent.Spec.Workspace.LimitRange.Container.DefaultRequest) ||
		!hasComputeDefaults(agent.Spec.Workspace.LimitRange.Container.Default) {
		return nil, "LimitRange defaults must include CPU and memory requests and limits", nil
	}

	membership := agent.Spec.Memberships[0]
	organization := &linkv1alpha1.Organization{}
	if err := r.Get(ctx, types.NamespacedName{Name: membership.Organization}, organization); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Sprintf("Organization %q does not exist", membership.Organization), nil
		}
		return nil, "", err
	}
	if !apiMeta.IsStatusConditionTrue(organization.Status.Conditions, linkv1alpha1.OrganizationConditionAccepted) {
		return organization, fmt.Sprintf("Organization %q is not accepted", membership.Organization), nil
	}
	projects := make(map[string]struct{}, len(organization.Spec.Projects))
	for _, project := range organization.Spec.Projects {
		projects[project.Name] = struct{}{}
	}
	for _, project := range membership.Projects {
		if _, found := projects[project]; !found {
			return organization, fmt.Sprintf("Project %q does not exist in organization %q", project, membership.Organization), nil
		}
	}
	return organization, "", nil
}

func (r *AgentReconciler) missingCredentialReferences(
	ctx context.Context,
	agent *linkv1alpha1.Agent,
) ([]string, error) {
	reader := r.APIReader
	if reader == nil {
		reader = r.Client
	}
	missing := make([]string, 0)
	for _, reference := range agent.Spec.Credentials {
		name, kind := credentialObject(reference)
		metadata := &metav1.PartialObjectMetadata{}
		metadata.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: kind})
		err := reader.Get(ctx, types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: name}, metadata)
		if apierrors.IsNotFound(err) {
			missing = append(missing, kind+"/"+name)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	slices.Sort(missing)
	return missing, nil
}

func credentialObject(reference linkv1alpha1.CredentialReference) (string, string) {
	if reference.Secret != nil {
		return *reference.Secret, credentialKindSecret
	}
	if reference.ConfigMap != nil {
		return *reference.ConfigMap, credentialKindConfig
	}
	return "", "Unknown"
}

func (r *AgentReconciler) finalizeAgent(ctx context.Context, agent *linkv1alpha1.Agent) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(agent, linkv1alpha1.AgentFinalizer) {
		return ctrl.Result{}, nil
	}
	namespace := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: agentNamespace(agent.Name)}, namespace)
	if err == nil && namespace.Labels[AgentUIDLabel] == string(agent.UID) {
		if namespace.DeletionTimestamp.IsZero() {
			if err := r.Delete(ctx, namespace); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	base := agent.DeepCopy()
	controllerutil.RemoveFinalizer(agent, linkv1alpha1.AgentFinalizer)
	return ctrl.Result{}, r.Patch(ctx, agent, client.MergeFrom(base))
}

func requiredQuotaKeysMissing(hard corev1.ResourceList) []string {
	required := []corev1.ResourceName{
		corev1.ResourceRequestsCPU,
		corev1.ResourceRequestsMemory,
		corev1.ResourceLimitsCPU,
		corev1.ResourceLimitsMemory,
		corev1.ResourceRequestsStorage,
		corev1.ResourcePersistentVolumeClaims,
		corev1.ResourceName("count/pods"),
		corev1.ResourceName("count/jobs.batch"),
		corev1.ResourceName("count/services"),
		corev1.ResourceName("count/configmaps"),
		corev1.ResourceName("count/secrets"),
	}
	missing := make([]string, 0)
	for _, resourceName := range required {
		quantity, found := hard[resourceName]
		if !found || quantity.Sign() <= 0 {
			missing = append(missing, string(resourceName))
		}
	}
	return missing
}

func hasComputeDefaults(resources corev1.ResourceList) bool {
	cpu, cpuFound := resources[corev1.ResourceCPU]
	memory, memoryFound := resources[corev1.ResourceMemory]
	return cpuFound && memoryFound && cpu.Sign() > 0 && memory.Sign() > 0
}

func storageQuotaValidationMessage(agent *linkv1alpha1.Agent) string {
	workspaceSize := agent.Spec.Workspace.Volume.Size.DeepCopy()
	if workspaceSize.IsZero() {
		workspaceSize = defaultWorkspaceSize.DeepCopy()
	}
	requiredStorage := workspaceSize.DeepCopy()
	requiredStorage.Add(defaultNixStoreSize)
	storageQuota := agent.Spec.Workspace.ResourceQuota.Hard[corev1.ResourceRequestsStorage]
	if storageQuota.Cmp(requiredStorage) < 0 {
		return fmt.Sprintf(
			"Resource quota requests.storage must be at least %s (workspace %s + Nix store %s)",
			requiredStorage.String(), workspaceSize.String(), defaultNixStoreSize.String(),
		)
	}
	pvcQuota := agent.Spec.Workspace.ResourceQuota.Hard[corev1.ResourcePersistentVolumeClaims]
	if pvcQuota.CmpInt64(2) < 0 {
		return "Resource quota persistentvolumeclaims must allow at least 2 claims (workspace + Nix store)"
	}
	return ""
}

func setAgentCondition(
	agent *linkv1alpha1.Agent,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) {
	apiMeta.SetStatusCondition(&agent.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: agent.Generation,
	})
}

func blockAgentConditions(agent *linkv1alpha1.Agent, from, reason, message string) {
	start := slices.Index(agentConditionOrder, from)
	if start < 0 {
		return
	}
	for _, conditionType := range agentConditionOrder[start:] {
		setAgentCondition(agent, conditionType, metav1.ConditionFalse, reason, message)
	}
}

func (r *AgentReconciler) finishAgent(
	ctx context.Context,
	base *linkv1alpha1.Agent,
	agent *linkv1alpha1.Agent,
	result ctrl.Result,
	reconcileErr error,
) (ctrl.Result, error) {
	if !apiequality.Semantic.DeepEqual(base.Status, agent.Status) {
		if err := r.Status().Patch(ctx, agent, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
	}
	return result, reconcileErr
}

func (r *AgentReconciler) mapManagedResource(
	_ context.Context,
	object client.Object,
) []reconcile.Request {
	name := object.GetLabels()[AgentNameLabel]
	if name == "" {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: name}}}
}

func (r *AgentReconciler) mapOrganization(
	ctx context.Context,
	object client.Object,
) []reconcile.Request {
	agents := &linkv1alpha1.AgentList{}
	if err := r.List(ctx, agents); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for i := range agents.Items {
		for _, membership := range agents.Items[i].Spec.Memberships {
			if membership.Organization == object.GetName() {
				requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: agents.Items[i].Name}})
				break
			}
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	managed := handler.EnqueueRequestsFromMapFunc(r.mapManagedResource)
	return ctrl.NewControllerManagedBy(mgr).
		For(&linkv1alpha1.Agent{}).
		Watches(&linkv1alpha1.Organization{}, handler.EnqueueRequestsFromMapFunc(r.mapOrganization)).
		Watches(&corev1.Namespace{}, managed).
		Watches(&corev1.ServiceAccount{}, managed).
		Watches(&corev1.ResourceQuota{}, managed).
		Watches(&corev1.LimitRange{}, managed).
		Watches(&corev1.PersistentVolumeClaim{}, managed).
		Watches(&rbacv1.RoleBinding{}, managed).
		Watches(&appsv1.Deployment{}, managed).
		Named("agent").
		Complete(r)
}
