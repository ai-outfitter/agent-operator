package controller

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

// OrganizationReconciler reconciles an Organization object.
type OrganizationReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	AgentImage   string
	GatewayImage string
}

// +kubebuilder:rbac:groups=aioutfitter.com,resources=organizations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aioutfitter.com,resources=organizations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aioutfitter.com,resources=organizations/finalizers,verbs=update
// +kubebuilder:rbac:groups=aioutfitter.com,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces;configmaps;secrets;services;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses;networkpolicies,verbs=get;list;watch;create;update;patch;delete

// Reconcile validates the organization source declaration without resolving it.
func (r *OrganizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	organization := &aioutfitterv1alpha1.Organization{}
	if err := r.Get(ctx, req.NamespacedName, organization); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !organization.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, organization)
	}
	if !controllerutil.ContainsFinalizer(organization, aioutfitterv1alpha1.OrganizationFinalizer) {
		base := organization.DeepCopy()
		controllerutil.AddFinalizer(organization, aioutfitterv1alpha1.OrganizationFinalizer)
		if err := r.Patch(ctx, organization, client.MergeFrom(base)); err != nil {
			return ctrl.Result{}, err
		}
	}

	statusBase := organization.DeepCopy()
	organization.Status.ObservedGeneration = organization.Generation
	organization.Status.ResolvedRepositories = resolvedRepositories(organization.Spec.Repositories)

	if validationMessage := validateOrganization(organization); validationMessage != "" {
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionAccepted, metav1.ConditionFalse, "InvalidSpecification", validationMessage)
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionCatalogSourcesReady, metav1.ConditionFalse, "Blocked", "Catalog source delegation is blocked by an invalid specification")
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionReady, metav1.ConditionFalse, "NotAccepted", "Organization is not accepted")
		return ctrl.Result{}, r.patchOrganizationStatus(ctx, statusBase, organization)
	}
	setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionAccepted, metav1.ConditionTrue, "Accepted", "Organization specification is valid")

	catalogSource := organization.Spec.AgentCatalogs[0]
	revision := ""
	if catalogSource.Revision != nil {
		revision = strings.ToLower(*catalogSource.Revision)
	}
	organization.Status.CatalogSources = []aioutfitterv1alpha1.CatalogSourceStatus{{
		Name:     catalogSource.Name,
		Revision: revision,
	}}
	setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionCatalogSourcesReady, metav1.ConditionTrue, "DelegatedToOutfitter", "Catalog source is pinned and ready for Outfitter settings")
	result, err := r.reconcileForgeGateway(ctx, organization)
	if err != nil {
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionReady, metav1.ConditionFalse, "ForgeReconcileFailed", "Forge gateway reconciliation failed")
		_ = r.patchOrganizationStatus(ctx, statusBase, organization)
		return ctrl.Result{}, err
	}
	ready := organization.Spec.Forge == nil || (conditionTrue(organization, aioutfitterv1alpha1.OrganizationConditionForgeGatewayReady) && conditionTrue(organization, aioutfitterv1alpha1.OrganizationConditionForgeRoutesReady) && conditionTrue(organization, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady))
	if ready {
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionReady, metav1.ConditionTrue, "Ready", "Organization is ready for agents")
	} else {
		setOrganizationCondition(organization, aioutfitterv1alpha1.OrganizationConditionReady, metav1.ConditionFalse, "ForgeNotReady", "Organization forge gateway is not ready")
	}

	return result, r.patchOrganizationStatus(ctx, statusBase, organization)
}

func conditionTrue(organization *aioutfitterv1alpha1.Organization, conditionType string) bool {
	condition := apiMeta.FindStatusCondition(organization.Status.Conditions, conditionType)
	return condition != nil && condition.Status == metav1.ConditionTrue
}

func (r *OrganizationReconciler) finalize(
	ctx context.Context,
	organization *aioutfitterv1alpha1.Organization,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(organization, aioutfitterv1alpha1.OrganizationFinalizer) {
		return ctrl.Result{}, nil
	}
	base := organization.DeepCopy()
	controllerutil.RemoveFinalizer(organization, aioutfitterv1alpha1.OrganizationFinalizer)
	return ctrl.Result{}, r.Patch(ctx, organization, client.MergeFrom(base))
}

func validateOrganization(organization *aioutfitterv1alpha1.Organization) string {
	const httpScheme = "http"
	if len(organization.Spec.AgentCatalogs) != 1 {
		return "M1 organizations must declare exactly one agent catalog"
	}
	for _, repository := range organization.Spec.Repositories {
		parsed, err := url.Parse(repository.URI)
		if err != nil || parsed.Scheme == "" {
			return fmt.Sprintf("Repository %q must use a valid clone URI", repository.Name)
		}
		if (parsed.Scheme == httpScheme || parsed.Scheme == "https") && parsed.User != nil {
			return fmt.Sprintf("Repository %q URI must not contain credentials", repository.Name)
		}
	}
	catalogSource := organization.Spec.AgentCatalogs[0]
	if catalogSource.URI != nil {
		parsed, err := url.Parse(*catalogSource.URI)
		if err != nil || parsed.Scheme == "" {
			return fmt.Sprintf("Catalog %q must use a valid clone URI", catalogSource.Name)
		}
		if (parsed.Scheme == httpScheme || parsed.Scheme == "https") && parsed.User != nil {
			return fmt.Sprintf("Catalog %q URI must not contain credentials", catalogSource.Name)
		}
	}
	if catalogSource.Path != "" {
		cleaned := path.Clean(catalogSource.Path)
		if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			return fmt.Sprintf("Catalog %q payload path must stay within its source", catalogSource.Name)
		}
	}
	return ""
}

func resolvedRepositories(repositories []aioutfitterv1alpha1.Repository) []aioutfitterv1alpha1.ResolvedRepositoryStatus {
	resolved := make([]aioutfitterv1alpha1.ResolvedRepositoryStatus, 0, len(repositories))
	for _, repository := range repositories {
		resolved = append(resolved, aioutfitterv1alpha1.ResolvedRepositoryStatus{
			Name:     repository.Name,
			Revision: repository.Revision,
		})
	}
	slices.SortFunc(resolved, func(a, b aioutfitterv1alpha1.ResolvedRepositoryStatus) int {
		return stringCompare(a.Name, b.Name)
	})
	return resolved
}

func stringCompare(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func setOrganizationCondition(
	organization *aioutfitterv1alpha1.Organization,
	conditionType string,
	status metav1.ConditionStatus,
	reason string,
	message string,
) {
	apiMeta.SetStatusCondition(&organization.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: organization.Generation,
	})
}

func (r *OrganizationReconciler) patchOrganizationStatus(
	ctx context.Context,
	base *aioutfitterv1alpha1.Organization,
	organization *aioutfitterv1alpha1.Organization,
) error {
	if apiequality.Semantic.DeepEqual(base.Status, organization.Status) {
		return nil
	}
	if err := r.Status().Patch(ctx, organization, client.MergeFrom(base)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OrganizationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aioutfitterv1alpha1.Organization{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Named("organization").
		Complete(r)
}
