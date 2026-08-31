package controller

import (
	"context"
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

var testNameCounter atomic.Uint64

const (
	testCatalogRevision = "0123456789abcdef0123456789abcdef01234567"
	testCatalogGitHub   = "ai-outfitter/agent-operator"
	testCatalogName     = "agents"
	testRepositoryName  = "wiki"
	testOutfitterOrg    = "ai-outfitter"
	testArteraOrg       = "artera"
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
		organization := &aioutfitterv1alpha1.Organization{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: aioutfitterv1alpha1.OrganizationSpec{
				Repositories: []aioutfitterv1alpha1.Repository{{
					Name: testRepositoryName, URI: "ssh://git@example.test/ai-outfitter/wiki.git",
				}},
				AgentCatalogs: []aioutfitterv1alpha1.AgentCatalog{{
					Name: testCatalogName, GitHub: &github, Revision: &revision, Path: ".agents",
				}},
			},
		}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)

		reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())

		actual := &aioutfitterv1alpha1.Organization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, actual)).To(Succeed())
		Expect(actual.Finalizers).To(ContainElement(aioutfitterv1alpha1.OrganizationFinalizer))
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, aioutfitterv1alpha1.OrganizationConditionAccepted)).To(BeTrue())
		catalogCondition := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.OrganizationConditionCatalogSourcesReady)
		Expect(catalogCondition).NotTo(BeNil())
		Expect(catalogCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(catalogCondition.Reason).To(Equal("DelegatedToOutfitter"))
		Expect(actual.Status.CatalogSources).To(Equal([]aioutfitterv1alpha1.CatalogSourceStatus{{
			Name: testCatalogName, Revision: revision,
		}}))
		Expect(actual.Status.ResolvedRepositories).To(Equal([]aioutfitterv1alpha1.ResolvedRepositoryStatus{{Name: testRepositoryName}}))
	})

	It("rejects credential-bearing repository URIs without echoing them", func() {
		name := uniqueTestName("organization")
		revision := testCatalogRevision
		github := testCatalogGitHub
		organization := &aioutfitterv1alpha1.Organization{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: aioutfitterv1alpha1.OrganizationSpec{
				Repositories:  []aioutfitterv1alpha1.Repository{{Name: testRepositoryName, URI: "https://private-token@example.test/wiki.git"}},
				AgentCatalogs: []aioutfitterv1alpha1.AgentCatalog{{Name: testCatalogName, GitHub: &github, Revision: &revision}},
			},
		}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)

		reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
		Expect(err).NotTo(HaveOccurred())

		actual := &aioutfitterv1alpha1.Organization{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, actual)).To(Succeed())
		accepted := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.OrganizationConditionAccepted)
		Expect(accepted).NotTo(BeNil())
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Message).To(ContainSubstring("must not contain credentials"))
		Expect(accepted.Message).NotTo(ContainSubstring("private-token"))
	})

	It("enqueues each distinct organization when an Agent membership changes", func() {
		reconciler := &OrganizationReconciler{}
		agent := &aioutfitterv1alpha1.Agent{Spec: aioutfitterv1alpha1.AgentSpec{Memberships: []aioutfitterv1alpha1.Membership{
			{Organization: testArteraOrg}, {Organization: testArteraOrg}, {Organization: testOutfitterOrg},
		}}}
		requests := reconciler.organizationsForAgent(ctx, agent)
		Expect(requests).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: testArteraOrg}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: testOutfitterOrg}},
		))
	})

	It("enqueues only the configured Organization credential Secret", func() {
		name := uniqueTestName("organization")
		revision := testCatalogRevision
		github := testCatalogGitHub
		organization := &aioutfitterv1alpha1.Organization{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: aioutfitterv1alpha1.OrganizationSpec{
			AgentCatalogs:        []aioutfitterv1alpha1.AgentCatalog{{Name: testCatalogName, GitHub: &github, Revision: &revision}},
			CredentialSecretName: "custom-org-credentials",
			Forge: &aioutfitterv1alpha1.OrganizationForgeSpec{
				Provider: "forgejo", Owner: "artera", ServerURL: "https://git.example.test",
			},
		}}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)
		reconciler := &OrganizationReconciler{Client: k8sClient}

		matching := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "custom-org-credentials", Namespace: "org-" + name}}
		Expect(reconciler.organizationForCredentialSecret(ctx, matching)).To(Equal([]reconcile.Request{{NamespacedName: types.NamespacedName{Name: name}}}))
		other := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "org-" + name}}
		Expect(reconciler.organizationForCredentialSecret(ctx, other)).To(BeEmpty())
	})

	It("uses the standard Secrets and leaves webhook exposure user-managed", func() {
		name := uniqueTestName("organization")
		revision := testCatalogRevision
		github := testCatalogGitHub
		organization := &aioutfitterv1alpha1.Organization{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: aioutfitterv1alpha1.OrganizationSpec{
			AgentCatalogs: []aioutfitterv1alpha1.AgentCatalog{{Name: testCatalogName, GitHub: &github, Revision: &revision}},
			Forge: &aioutfitterv1alpha1.OrganizationForgeSpec{
				Provider: "forgejo", Owner: "artera", ServerURL: "https://git.example.test",
			},
		}}
		Expect(k8sClient.Create(ctx, organization)).To(Succeed())
		DeferCleanup(removeOrganization, ctx, name)
		reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), GatewayImage: "example.test/agent-operator:v0.12.0"}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, organization)).To(Succeed())

		namespace := organizationNamespace(name)
		credentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultOrgCredentials, Namespace: namespace}, Data: map[string][]byte{
			forgeWebhookSecretKey:         []byte("webhook-hmac"),
			"default.SPARK_AUTHORIZATION": []byte("spark-token"),
		}}
		Expect(k8sClient.Create(ctx, credentials)).To(Succeed())
		agent := validAgent(uniqueTestName("agent"), name)
		agent.Spec.Forge = &aioutfitterv1alpha1.AgentForgeSpec{Username: "aster"}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: agentNamespace(agent.Name)}})).To(Succeed())

		legacyIngress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{
			Name: ForgeGatewayName, Namespace: namespace,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: aioutfitterv1alpha1.GroupVersion.String(), Kind: "Organization", Name: name, UID: organization.UID, Controller: ptr.To(true),
			}},
		}, Spec: networkingv1.IngressSpec{DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
			Name: ForgeGatewayName, Port: networkingv1.ServiceBackendPort{Number: 80},
		}}}}
		Expect(k8sClient.Create(ctx, legacyIngress)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ForgeGatewayName}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.AutomountServiceAccountToken).To(PointTo(BeFalse()))
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(reconciler.GatewayImage))
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElement(MatchFields(IgnoreExtras, Fields{
			"Name": Equal("FORGE_WEBHOOK_SECRET"),
			"ValueFrom": PointTo(MatchFields(IgnoreExtras, Fields{
				"SecretKeyRef": PointTo(MatchFields(IgnoreExtras, Fields{"Key": Equal(forgeWebhookSecretKey)})),
			})),
		})))
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ForgeGatewayName}, legacyIngress)).To(Succeed())
		Expect(legacyIngress.OwnerReferences).To(BeEmpty())

		agentCredentials := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: defaultAgentCredentials}, agentCredentials)).To(Succeed())
		Expect(agentCredentials.Data).To(HaveKey(agentA2ACredentialsKey))
		policy := &networkingv1.NetworkPolicy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: "forge-gateway-isolation"}, policy)).To(Succeed())
		Expect(policy.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeEgress}))
		Expect(policy.Spec.Ingress).To(BeEmpty())
	})
})

func removeOrganization(ctx context.Context, name string) {
	organization := &aioutfitterv1alpha1.Organization{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, organization)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	organization.Finalizers = nil
	Expect(k8sClient.Update(ctx, organization)).To(Succeed())
	Expect(k8sClient.Delete(ctx, organization)).To(Succeed())
}
