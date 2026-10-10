package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

// Inference provider Secret contract shared with the webapp gateway
// (ai-outfitter/webapp#222). See docs/inference-provider-secrets.md.
const (
	// OrganizationSlugLabel marks an organization namespace with its slug.
	OrganizationSlugLabel = "outfitter.ai/organization"
	// InferenceProviderLabel marks a provider Secret and names its kind.
	InferenceProviderLabel = "outfitter.ai/inference-provider"
	// ProviderSourceAnnotation records "<namespace>/<name>" of the canonical
	// Secret on each operator-owned replica.
	ProviderSourceAnnotation = "outfitter.ai/source"
	// ProviderChecksumAnnotation on the agent pod template changes whenever a
	// provider Secret replica changes, rolling env consumers.
	ProviderChecksumAnnotation = "outfitter.ai/inference-providers-checksum"
	// ProviderMountRoot holds one directory per provider Secret:
	// <root>/<source secret name>/{apiKey,baseUrl,model}.
	ProviderMountRoot       = "/var/run/outfitter/inference"
	providerVolumeName      = "inference-providers"
	providerCopyPrefix      = "inference-"
	organizationNamespacePx = "outfitter-org-"
)

// OrganizationProviderNamespace is the namespace holding an Organization's
// canonical provider Secrets. The Organization resource name is the slug.
func OrganizationProviderNamespace(slug string) string { return organizationNamespacePx + slug }

func providerCopyName(sourceName string) string { return providerCopyPrefix + sourceName }

// ProviderSecretReconciler ensures each Organization's provider namespace and
// syncs its labelled provider Secrets into the namespaces of member Agents.
//
// Workspaces carry no organization reference in the API, so they receive no
// copies yet.
type ProviderSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=aioutfitter.com,resources=organizations,verbs=get;list;watch
// +kubebuilder:rbac:groups=aioutfitter.com,resources=agents,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile converges the provider Secret copies of one Organization.
func (r *ProviderSecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	slug := req.Name
	organization := &aioutfitterv1alpha1.Organization{}
	if err := r.Get(ctx, req.NamespacedName, organization); err != nil {
		// Copies are owned by the Organization; garbage collection removes them.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !organization.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	sourceNamespace := OrganizationProviderNamespace(slug)
	if len(sourceNamespace) > 63 {
		return ctrl.Result{}, fmt.Errorf("organization %q is too long for namespace %q", slug, sourceNamespace)
	}
	if err := r.ensureNamespace(ctx, sourceNamespace, slug); err != nil {
		return ctrl.Result{}, err
	}

	sources := &corev1.SecretList{}
	if err := r.List(ctx, sources, client.InNamespace(sourceNamespace), client.HasLabels{InferenceProviderLabel}); err != nil {
		return ctrl.Result{}, err
	}
	agents := &aioutfitterv1alpha1.AgentList{}
	if err := r.List(ctx, agents); err != nil {
		return ctrl.Result{}, err
	}

	desired := map[types.NamespacedName]struct{}{}
	for i := range agents.Items {
		agent := &agents.Items[i]
		if !agent.DeletionTimestamp.IsZero() || !isMemberOf(agent, slug) {
			continue
		}
		namespace := agentNamespace(agent.Name)
		if err := r.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{}); err != nil {
			if apierrors.IsNotFound(err) {
				continue // the Agent controller has not created it yet
			}
			return ctrl.Result{}, err
		}
		for j := range sources.Items {
			source := &sources.Items[j]
			if !source.DeletionTimestamp.IsZero() {
				continue
			}
			key := types.NamespacedName{Namespace: namespace, Name: providerCopyName(source.Name)}
			desired[key] = struct{}{}
			if err := r.ensureCopy(ctx, organization, agent, source, key); err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	copies := &corev1.SecretList{}
	if err := r.List(ctx, copies, client.MatchingLabels{OrganizationSlugLabel: slug}, client.HasLabels{InferenceProviderLabel}); err != nil {
		return ctrl.Result{}, err
	}
	for i := range copies.Items {
		replica := &copies.Items[i]
		if replica.Namespace == sourceNamespace || replica.Annotations[ProviderSourceAnnotation] == "" {
			continue
		}
		if _, keep := desired[client.ObjectKeyFromObject(replica)]; keep {
			continue
		}
		if err := r.Delete(ctx, replica); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func isMemberOf(agent *aioutfitterv1alpha1.Agent, slug string) bool {
	return slices.ContainsFunc(agent.Spec.Memberships, func(m aioutfitterv1alpha1.Membership) bool {
		return m.Organization == slug
	})
}

// ensureNamespace creates the organization namespace or labels an existing
// one (the webapp gateway may have created it). It is deliberately not owned
// by the Organization: deleting the resource must not delete the canonical
// Secrets the webapp wrote.
func (r *ProviderSecretReconciler) ensureNamespace(ctx context.Context, name, slug string) error {
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	_, err := controllerutil.CreateOrPatch(ctx, r.Client, namespace, func() error {
		namespace.Labels = mergeLabels(namespace.Labels, map[string]string{OrganizationSlugLabel: slug})
		return nil
	})
	return err
}

func (r *ProviderSecretReconciler) ensureCopy(
	ctx context.Context,
	organization *aioutfitterv1alpha1.Organization,
	agent *aioutfitterv1alpha1.Agent,
	source *corev1.Secret,
	key types.NamespacedName,
) error {
	replica := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	sourceRef := source.Namespace + "/" + source.Name
	if err := r.Get(ctx, key, replica); client.IgnoreNotFound(err) != nil {
		return err
	}
	if replica.ResourceVersion != "" && replica.Annotations[ProviderSourceAnnotation] != sourceRef {
		// Never overwrite a Secret this controller did not create.
		return fmt.Errorf("secret %s exists and is not a provider replica of %s", key, sourceRef)
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, replica, func() error {
		labels := maps.Clone(source.Labels)
		if labels == nil {
			labels = map[string]string{}
		}
		maps.Copy(labels, ownershipLabels(agent))
		labels[OrganizationSlugLabel] = organization.Name
		replica.Labels = labels
		replica.Annotations = mergeLabels(replica.Annotations, map[string]string{ProviderSourceAnnotation: sourceRef})
		replica.Type = source.Type
		replica.Data = maps.Clone(source.Data)
		return controllerutil.SetOwnerReference(organization, replica, r.Scheme)
	})
	return err
}

// providerSecretVolume projects every provider replica in the agent namespace
// into one volume. Secret volumes update in place on rotation; the checksum
// rolls the pod for consumers that read them at start-up.
func providerSecretVolume(copies []corev1.Secret) (corev1.Volume, string, bool) {
	if len(copies) == 0 {
		return corev1.Volume{}, "", false
	}
	slices.SortFunc(copies, func(a, b corev1.Secret) int { return strings.Compare(a.Name, b.Name) })
	sources := make([]corev1.VolumeProjection, 0, len(copies))
	hash := sha256.New()
	for _, replica := range copies {
		dir := strings.TrimPrefix(replica.Name, providerCopyPrefix)
		keys := slices.Sorted(maps.Keys(replica.Data))
		items := make([]corev1.KeyToPath, 0, len(keys))
		_, _ = fmt.Fprintf(hash, "%s\x00", replica.Name)
		for _, k := range keys {
			items = append(items, corev1.KeyToPath{Key: k, Path: path.Join(dir, k)})
			_, _ = fmt.Fprintf(hash, "%s\x00%d\x00", k, len(replica.Data[k]))
			_, _ = hash.Write(replica.Data[k])
		}
		sources = append(sources, corev1.VolumeProjection{Secret: &corev1.SecretProjection{
			LocalObjectReference: corev1.LocalObjectReference{Name: replica.Name},
			Items:                items,
		}})
	}
	volume := corev1.Volume{
		Name:         providerVolumeName,
		VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: sources}},
	}
	return volume, fmt.Sprintf("%x", hash.Sum(nil)), true
}

// listProviderCopies returns the operator-owned provider copies in an agent namespace.
func listProviderCopies(ctx context.Context, c client.Reader, namespace string) ([]corev1.Secret, error) {
	list := &corev1.SecretList{}
	if err := c.List(ctx, list, client.InNamespace(namespace), client.HasLabels{InferenceProviderLabel}); err != nil {
		return nil, err
	}
	copies := make([]corev1.Secret, 0, len(list.Items))
	for _, secret := range list.Items {
		if secret.Annotations[ProviderSourceAnnotation] != "" && secret.DeletionTimestamp.IsZero() {
			copies = append(copies, secret)
		}
	}
	return copies, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProviderSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	providerSecrets := builder.WithPredicates(predicate.NewPredicateFuncs(func(object client.Object) bool {
		_, ok := object.GetLabels()[InferenceProviderLabel]
		return ok
	}))
	return ctrl.NewControllerManagedBy(mgr).
		For(&aioutfitterv1alpha1.Organization{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(organizationForProviderSecret), providerSecrets).
		Watches(&aioutfitterv1alpha1.Agent{}, handler.EnqueueRequestsFromMapFunc(organizationsForAgent)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.organizationsForAgentNamespace)).
		Named("providersecret").
		Complete(r)
}

// organizationForProviderSecret maps a canonical Secret (by namespace) or a
// replica (by its organization label) to its Organization.
func organizationForProviderSecret(_ context.Context, object client.Object) []reconcile.Request {
	if slug, ok := strings.CutPrefix(object.GetNamespace(), organizationNamespacePx); ok {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: slug}}}
	}
	if slug := object.GetLabels()[OrganizationSlugLabel]; slug != "" {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: slug}}}
	}
	return nil
}

func organizationsForAgent(_ context.Context, object client.Object) []reconcile.Request {
	agent, ok := object.(*aioutfitterv1alpha1.Agent)
	if !ok {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(agent.Spec.Memberships))
	for _, membership := range agent.Spec.Memberships {
		requests = append(requests, reconcile.Request{NamespacedName: types.NamespacedName{Name: membership.Organization}})
	}
	return requests
}

// organizationsForAgentNamespace resyncs once an Agent namespace appears,
// since copies are only written into existing namespaces.
func (r *ProviderSecretReconciler) organizationsForAgentNamespace(ctx context.Context, object client.Object) []reconcile.Request {
	name := object.GetLabels()[AgentNameLabel]
	if name == "" {
		return nil
	}
	agent := &aioutfitterv1alpha1.Agent{}
	if err := r.Get(ctx, types.NamespacedName{Name: name}, agent); err != nil {
		return nil
	}
	return organizationsForAgent(ctx, agent)
}
