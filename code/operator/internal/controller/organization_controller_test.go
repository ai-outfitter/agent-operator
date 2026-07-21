package controller

import (
	"context"
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	linkv1alpha1 "github.com/ncrmro/link-operator/code/operator/api/v1alpha1"
)

var testNameCounter atomic.Uint64

const (
	testCatalogRevision = "0123456789abcdef0123456789abcdef01234567"
	testCatalogGitHub   = "ncrmro/link-operator"
	testCatalogName     = "agents"
	testRepositoryName  = "wiki"
)

func uniqueTestName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, testNameCounter.Add(1))
}

var _ = Describe("Organization Controller", func() {
	ctx := context.Background()

	It("accepts one pinned catalog without resolving the profile itself", func() {
		name := uniqueTestName("organization")
		revision := testCatalogRevision
		github := testCatalogGitHub
		organization := &linkv1alpha1.Organization{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: linkv1alpha1.OrganizationSpec{
				Repositories: []linkv1alpha1.Repository{{
					Name: testRepositoryName, URI: "ssh://git@example.test/ai-outfitter/wiki.git",
				}},
				AgentCatalogs: []linkv1alpha1.AgentCatalog{{
					Name: testCatalogName, GitHub: &github, Revision: &revision, Path: ".agents",
				}},
			},
		}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)

		reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())

		actual := &linkv1alpha1.Organization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(linkv1alpha1.OrganizationFinalizer))
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, linkv1alpha1.OrganizationConditionAccepted)).To(BeTrue())
		catalogCondition := apiMeta.FindStatusCondition(actual.Status.Conditions, linkv1alpha1.OrganizationConditionCatalogSourcesReady)
		Expect(catalogCondition).NotTo(BeNil())
		Expect(catalogCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(catalogCondition.Reason).To(Equal("DelegatedToOutfitter"))
		Expect(actual.Status.CatalogSources).To(Equal([]linkv1alpha1.CatalogSourceStatus{{
			Name: testCatalogName, Revision: revision,
		}}))
		Expect(actual.Status.ResolvedRepositories).To(Equal([]linkv1alpha1.ResolvedRepositoryStatus{{Name: testRepositoryName}}))
	})

	It("rejects credential-bearing repository URIs without echoing them", func() {
		name := uniqueTestName("organization")
		revision := testCatalogRevision
		github := testCatalogGitHub
		organization := &linkv1alpha1.Organization{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: linkv1alpha1.OrganizationSpec{
				Repositories:  []linkv1alpha1.Repository{{Name: testRepositoryName, URI: "https://private-token@example.test/wiki.git"}},
				AgentCatalogs: []linkv1alpha1.AgentCatalog{{Name: testCatalogName, GitHub: &github, Revision: &revision}},
			},
		}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)

		reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())

		actual := &linkv1alpha1.Organization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, actual)).To(Succeed())
		accepted := apiMeta.FindStatusCondition(actual.Status.Conditions, linkv1alpha1.OrganizationConditionAccepted)
		Expect(accepted).NotTo(BeNil())
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Message).To(ContainSubstring("must not contain credentials"))
		Expect(accepted.Message).NotTo(ContainSubstring("private-token"))
	})
})

func removeOrganization(ctx context.Context, name string) {
	organization := &linkv1alpha1.Organization{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, organization)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	organization.Finalizers = nil
	Expect(k8sClient.Update(ctx, organization)).To(Succeed())
	Expect(k8sClient.Delete(ctx, organization)).To(Succeed())
}
