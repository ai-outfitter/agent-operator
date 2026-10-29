package controller

import (
	"context"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const testAPIKey = "apiKey"

var _ = Describe("Provider Secret sync", func() {
	ctx := context.Background()

	It("syncs provider Secrets to member agents only and rolls the runtime", func() {
		organization := createAcceptedOrganization(ctx)
		other := createAcceptedOrganization(ctx)
		agentReconciler := &AgentReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), RelayImage: testRelay, InferenceGatewayURL: testInferenceGateway, InferenceModel: testInferenceModel, AgentImage: testAgentImage}
		member := validAgent(uniqueTestName("provider-member"), organization.Name)
		outsider := validAgent(uniqueTestName("provider-outsider"), other.Name)
		for _, agent := range []string{member.Name, outsider.Name} {
			DeferCleanup(removeAgent, ctx, agent)
		}
		Expect(k8sClient.Create(ctx, member)).To(Succeed())
		Expect(k8sClient.Create(ctx, outsider)).To(Succeed())
		reconcileAgent := func(name string) {
			_, err := agentReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
			Expect(err).NotTo(HaveOccurred())
		}
		reconcileAgent(member.Name)
		reconcileAgent(outsider.Name)

		sync := &ProviderSecretReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		reconcileOrg := func(name string) {
			_, err := sync.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
			Expect(err).NotTo(HaveOccurred())
		}
		reconcileOrg(organization.Name)
		reconcileOrg(other.Name)

		sourceNamespace := OrganizationProviderNamespace(organization.Name)
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sourceNamespace}, namespace)).To(Succeed())
		Expect(namespace.Labels).To(HaveKeyWithValue(OrganizationSlugLabel, organization.Name))

		source := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "openai-org", Namespace: sourceNamespace, Labels: map[string]string{
				InferenceProviderLabel: "openai", ProviderOwnerKindLabel: ProviderOwnerOrganization, "outfitter.ai/owner-id": "org-1",
			}},
			Data: map[string][]byte{testAPIKey: []byte("v1"), "baseUrl": []byte("https://api.example.test"), "model": []byte("m")},
		}
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		reconcileOrg(organization.Name)
		reconcileOrg(other.Name)

		copyKey := types.NamespacedName{Namespace: agentNamespace(member.Name), Name: providerCopyName(source.Name)}
		replica := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, copyKey, replica)).To(Succeed())
		Expect(replica.Data).To(Equal(source.Data))
		Expect(replica.Labels).To(HaveKeyWithValue(InferenceProviderLabel, "openai"))
		Expect(replica.Labels).To(HaveKeyWithValue("outfitter.ai/owner-id", "org-1"))
		Expect(replica.Labels).To(HaveKeyWithValue(OrganizationSlugLabel, organization.Name))
		Expect(replica.Annotations).To(HaveKeyWithValue(ProviderSourceAnnotation, sourceNamespace+"/"+source.Name))
		Expect(replica.OwnerReferences).To(HaveLen(1))
		Expect(replica.OwnerReferences[0].UID).To(Equal(organization.UID))

		By("keeping a member's own key out of agent namespaces")
		personal := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "openai-user", Namespace: sourceNamespace, Labels: map[string]string{
				InferenceProviderLabel: "openai", ProviderOwnerKindLabel: "user", "outfitter.ai/owner-id": "user-1",
			}},
			Data: map[string][]byte{testAPIKey: []byte("mine")},
		}
		Expect(k8sClient.Create(ctx, personal)).To(Succeed())
		reconcileOrg(organization.Name)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
			Namespace: agentNamespace(member.Name), Name: providerCopyName(personal.Name),
		}, &corev1.Secret{}))).To(BeTrue())

		outsiderCopies, err := listProviderCopies(ctx, k8sClient, agentNamespace(outsider.Name))
		Expect(err).NotTo(HaveOccurred())
		Expect(outsiderCopies).To(BeEmpty())

		deploymentKey := types.NamespacedName{Namespace: agentNamespace(member.Name), Name: RuntimeName}
		deployment := &appsv1.Deployment{}
		reconcileAgent(member.Name)
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		firstChecksum := deployment.Spec.Template.Annotations[ProviderChecksumAnnotation]
		Expect(firstChecksum).NotTo(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(
			corev1.VolumeMount{Name: providerVolumeName, MountPath: ProviderMountRoot, ReadOnly: true}))
		volume := deployment.Spec.Template.Spec.Volumes[len(deployment.Spec.Template.Spec.Volumes)-1]
		Expect(volume.Name).To(Equal(providerVolumeName))
		Expect(volume.Projected.Sources[0].Secret.Name).To(Equal(copyKey.Name))
		Expect(volume.Projected.Sources[0].Secret.Items).To(ContainElement(corev1.KeyToPath{Key: testAPIKey, Path: "openai-org/apiKey"}))

		By("propagating a rotation and changing the checksum")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(source), source)).To(Succeed())
		source.Data[testAPIKey] = []byte("v2")
		Expect(k8sClient.Update(ctx, source)).To(Succeed())
		reconcileOrg(organization.Name)
		Expect(k8sClient.Get(ctx, copyKey, replica)).To(Succeed())
		Expect(replica.Data[testAPIKey]).To(Equal([]byte("v2")))
		reconcileAgent(member.Name)
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Annotations[ProviderChecksumAnnotation]).NotTo(Equal(firstChecksum))

		By("reconciling an organization the agent left while it still holds its copies")
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(member), member)).To(Succeed())
		member.Spec.Memberships[0].Organization = other.Name
		Expect(sync.organizationsForAgent(ctx, member)).To(ContainElement(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: organization.Name}}))

		By("removing copies when the source is deleted")
		Expect(k8sClient.Delete(ctx, source)).To(Succeed())
		reconcileOrg(organization.Name)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, copyKey, replica))).To(BeTrue())
		reconcileAgent(member.Name)
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Annotations).NotTo(HaveKey(ProviderChecksumAnnotation))
		for _, v := range deployment.Spec.Template.Spec.Volumes {
			Expect(v.Name).NotTo(Equal(providerVolumeName))
		}
	})

	It("copies only hosting-primary provider keys and removes stale secondary collisions", func() {
		primary := createAcceptedOrganization(ctx)
		secondary := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("primary-provider"), primary.Name)
		agent.Spec.Memberships = append(agent.Spec.Memberships, aioutfitterv1alpha1.Membership{Organization: secondary.Name})
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)
		Expect((&AgentReconciler{Client: k8sClient}).ensureAgentNamespace(ctx, agent)).To(Succeed())
		sync := &ProviderSecretReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		reconcileOrg := func(name string) {
			_, err := sync.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
			Expect(err).NotTo(HaveOccurred())
		}
		for _, name := range []string{primary.Name, secondary.Name} {
			reconcileOrg(name)
		}
		newSource := func(org, name, marker string) *corev1.Secret {
			source := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: OrganizationProviderNamespace(org), Labels: map[string]string{InferenceProviderLabel: "test", ProviderOwnerKindLabel: ProviderOwnerOrganization}}, Data: map[string][]byte{testAPIKey: []byte(marker)}}
			Expect(k8sClient.Create(ctx, source)).To(Succeed())
			return source
		}
		primarySource := newSource(primary.Name, "shared", "primary-marker")
		secondarySource := newSource(secondary.Name, "shared", "secondary-marker")
		secondaryOnly := newSource(secondary.Name, "secondary-only", "secondary-only-marker")
		sharedKey := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: providerCopyName("shared")}
		secondaryKey := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: providerCopyName("secondary-only")}
		reconcileOrg(secondary.Name)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, sharedKey, &corev1.Secret{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, secondaryKey, &corev1.Secret{}))).To(BeTrue())

		By("cleaning existing secondary replicas without dropping cleanup enqueue coverage")
		Expect(sync.ensureCopy(ctx, secondary, agent, secondarySource, sharedKey)).To(Succeed())
		Expect(sync.ensureCopy(ctx, secondary, agent, secondaryOnly, secondaryKey)).To(Succeed())
		withoutSecondary := agent.DeepCopy()
		withoutSecondary.Spec.Memberships = withoutSecondary.Spec.Memberships[:1]
		Expect(sync.organizationsForAgent(ctx, withoutSecondary)).To(ContainElements(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: primary.Name}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: secondary.Name}},
		))
		// Primary-first observes the old collision. The secondary deletion event
		// must then enqueue the primary to recreate its copy.
		staleReplica := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, sharedKey, staleReplica)).To(Succeed())
		reconcileOrg(primary.Name)
		reconcileOrg(secondary.Name)
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, secondaryKey, &corev1.Secret{}))).To(BeTrue())
		requests := sync.organizationForProviderSecret(ctx, staleReplica)
		Expect(requests).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: primary.Name}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: secondary.Name}},
		))
		for _, request := range requests {
			reconcileOrg(request.Name)
		}
		replica := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, sharedKey, replica)).To(Succeed())
		Expect(replica.Data).To(Equal(primarySource.Data))
		Expect(replica.Labels[OrganizationSlugLabel]).To(Equal(primary.Name))
		Expect(replica.OwnerReferences).To(HaveLen(1))
		Expect(replica.OwnerReferences[0].UID).To(Equal(primary.UID))

		By("keeping primary ownership after any further secondary reconciliation")
		reconcileOrg(secondary.Name)
		reconcileOrg(primary.Name)
		Expect(k8sClient.Get(ctx, sharedKey, replica)).To(Succeed())
		Expect(replica.Data).To(Equal(primarySource.Data))
	})

	It("skips, without overwriting, a Secret it does not own", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("provider-collide"), organization.Name)
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: agentNamespace(agent.Name)}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: OrganizationProviderNamespace(organization.Name)}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: providerCopyName("anthropic"), Namespace: agentNamespace(agent.Name),
		}, Data: map[string][]byte{testAPIKey: []byte("user")}})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: "anthropic", Namespace: OrganizationProviderNamespace(organization.Name),
			Labels: map[string]string{InferenceProviderLabel: "anthropic", ProviderOwnerKindLabel: ProviderOwnerOrganization},
		}, Data: map[string][]byte{testAPIKey: []byte("org")}})).To(Succeed())

		sync := &ProviderSecretReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := sync.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: organization.Name}})
		Expect(err).NotTo(HaveOccurred(), "a collision is skipped, not fatal")
		existing := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: providerCopyName("anthropic")}, existing)).To(Succeed())
		Expect(existing.Data[testAPIKey]).To(Equal([]byte("user")))
	})
})
