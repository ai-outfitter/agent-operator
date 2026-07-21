package controller

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	linkv1alpha1 "github.com/ncrmro/link-operator/code/operator/api/v1alpha1"
)

var _ = Describe("Agent Controller", func() {
	ctx := context.Background()

	It("reconciles a bounded workspace and waits for referenced objects", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("researcher"), organization.Name)
		secretName := "model-credentials"
		configName := "runtime-config"
		agent.Spec.Credentials = []linkv1alpha1.CredentialReference{
			{Secret: &secretName, As: linkv1alpha1.CredentialExposureEnv},
			{ConfigMap: &configName, As: linkv1alpha1.CredentialExposureVolume},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client:            k8sClient,
			APIReader:         k8sClient,
			Scheme:            k8sClient.Scheme(),
			AgentImage:        "example.test/link-agent@sha256:" + strings.Repeat("a", 64),
			OutfitterRevision: "c44205ef35265c893ad9f088772c35c71753bfb7",
		}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		namespaceName := agentNamespace(agent.Name)
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: namespaceName}, namespace)).To(Succeed())
		Expect(namespace.Labels).To(HaveKeyWithValue(AgentNameLabel, agent.Name))
		Expect(namespace.Labels).To(HaveKeyWithValue(AgentUIDLabel, string(agent.UID)))

		serviceAccount := &corev1.ServiceAccount{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}, serviceAccount)).To(Succeed())
		roleBinding := &rbacv1.RoleBinding{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: RuntimeName + "-admin"}, roleBinding)).To(Succeed())
		Expect(roleBinding.RoleRef).To(Equal(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "admin"}))

		quota := &corev1.ResourceQuota{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: WorkspaceName}, quota)).To(Succeed())
		Expect(quota.Spec.Hard).To(Equal(agent.Spec.Workspace.ResourceQuota.Hard))
		limitRange := &corev1.LimitRange{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: LimitRangeName}, limitRange)).To(Succeed())
		Expect(limitRange.Spec.Limits).To(HaveLen(1))
		claim := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: WorkspaceName}, claim)).To(Succeed())
		Expect(claim.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("10Gi")))

		settings := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: SettingsName}, settings)).To(Succeed())
		settingsYAML := settings.Data["settings.yml"]
		Expect(settingsYAML).To(ContainSubstring("default_agent: researcher"))
		Expect(settingsYAML).To(ContainSubstring("default_harness: pi"))
		Expect(settingsYAML).To(ContainSubstring("github: " + testCatalogGitHub))
		Expect(settingsYAML).To(ContainSubstring("ref: " + testCatalogRevision))
		Expect(settingsYAML).To(ContainSubstring("path: .agents"))

		actual := &linkv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		credentialsReady := apiMeta.FindStatusCondition(actual.Status.Conditions, linkv1alpha1.AgentConditionCredentialsReady)
		Expect(credentialsReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(credentialsReady.Message).To(ContainSubstring(credentialKindSecret + "/model-credentials"))
		Expect(credentialsReady.Message).To(ContainSubstring(credentialKindConfig + "/runtime-config"))
		Expect(apiMeta.IsStatusConditionFalse(actual.Status.Conditions, linkv1alpha1.AgentConditionWorkloadReady)).To(BeTrue())
		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}, deployment)).To(Satisfy(apierrors.IsNotFound))
	})

	It("projects references and becomes ready after the agent runtime starts", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("researcher"), organization.Name)
		secretName := "model-credentials"
		configName := "runtime-config"
		agent.Spec.Credentials = []linkv1alpha1.CredentialReference{
			{Secret: &secretName, As: linkv1alpha1.CredentialExposureEnv},
			{ConfigMap: &configName, As: linkv1alpha1.CredentialExposureVolume},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), AgentImage: "link-agent:test",
		}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		namespaceName := agentNamespace(agent.Name)
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespaceName},
			Data:       map[string][]byte{"providerToken": []byte("must-not-be-inspected")},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: namespaceName},
			Data:       map[string]string{"route": "example"},
		})).To(Succeed())

		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		deployment := &appsv1.Deployment{}
		deploymentKey := types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		container := deployment.Spec.Template.Spec.Containers[0]
		Expect(container.Command).To(BeEmpty())
		Expect(container.Args).To(BeEmpty())
		Expect(container.Env).To(ContainElements(
			corev1.EnvVar{Name: "LINK_AGENT_SLUG", Value: "researcher"},
			corev1.EnvVar{Name: "LINK_AGENT_HARNESS", Value: "pi"},
		))
		Expect(container.ReadinessProbe.Exec.Command).To(Equal([]string{"test", "-s", "/workspace/.link/mail-loop-ready"}))
		Expect(container.EnvFrom).To(ContainElement(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: "config-runtime-config", MountPath: CredentialsRoot + "/configmaps/runtime-config", ReadOnly: true,
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: SettingsName, MountPath: WorkspaceMount + "/.agents", ReadOnly: true,
		}))

		actual := &linkv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		settingsReady := apiMeta.FindStatusCondition(actual.Status.Conditions, linkv1alpha1.AgentConditionOutfitterSettingsReady)
		Expect(settingsReady.Status).To(Equal(metav1.ConditionTrue))
		Expect(settingsReady.Reason).To(Equal("Ready"))

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, linkv1alpha1.AgentConditionOutfitterSettingsReady)).To(BeTrue())
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, linkv1alpha1.AgentConditionReady)).To(BeTrue())
	})

	It("repairs an agent-weakened quota", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("guardrail"), organization.Name)
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)
		reconciler := &AgentReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		quota := &corev1.ResourceQuota{}
		key := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: WorkspaceName}
		Expect(k8sClient.Get(ctx, key, quota)).To(Succeed())
		quota.Spec.Hard[corev1.ResourceRequestsCPU] = resource.MustParse("999")
		Expect(k8sClient.Update(ctx, quota)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, quota)).To(Succeed())
		Expect(quota.Spec.Hard[corev1.ResourceRequestsCPU]).To(Equal(resource.MustParse("4")))
	})
})

func createAcceptedOrganization(ctx context.Context) *linkv1alpha1.Organization {
	name := uniqueTestName("organization")
	revision := testCatalogRevision
	github := testCatalogGitHub
	organization := &linkv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: linkv1alpha1.OrganizationSpec{AgentCatalogs: []linkv1alpha1.AgentCatalog{{
			Name: testCatalogName, GitHub: &github, Revision: &revision, Path: ".agents",
		}}},
	}
	Expect(k8sClient.Create(ctx, organization)).To(Succeed())
	reconciler := &OrganizationReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
	_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, organization)).To(Succeed())
	DeferCleanup(removeOrganization, ctx, name)
	return organization
}

func validAgent(name, organization string) *linkv1alpha1.Agent {
	return &linkv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: linkv1alpha1.AgentSpec{
			Memberships: []linkv1alpha1.Membership{{Organization: organization}},
			Profile:     linkv1alpha1.AgentProfile{Agent: "researcher", Harness: "pi"},
			Workspace: linkv1alpha1.WorkspaceSpec{
				ResourceQuota: linkv1alpha1.ResourceQuotaSpec{Hard: corev1.ResourceList{
					corev1.ResourceRequestsCPU:              resource.MustParse("4"),
					corev1.ResourceRequestsMemory:           resource.MustParse("8Gi"),
					corev1.ResourceLimitsCPU:                resource.MustParse("8"),
					corev1.ResourceLimitsMemory:             resource.MustParse("16Gi"),
					corev1.ResourceRequestsStorage:          resource.MustParse("50Gi"),
					corev1.ResourcePersistentVolumeClaims:   resource.MustParse("8"),
					corev1.ResourceName("count/pods"):       resource.MustParse("20"),
					corev1.ResourceName("count/jobs.batch"): resource.MustParse("50"),
					corev1.ResourceName("count/services"):   resource.MustParse("10"),
					corev1.ResourceName("count/configmaps"): resource.MustParse("50"),
					corev1.ResourceName("count/secrets"):    resource.MustParse("20"),
				}},
				LimitRange: linkv1alpha1.WorkspaceLimitRangeSpec{Container: linkv1alpha1.ContainerLimitSpec{
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Default: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				}},
			},
		},
	}
}

func removeAgent(ctx context.Context, name string) {
	agent := &linkv1alpha1.Agent{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, agent)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	agent.Finalizers = nil
	Expect(k8sClient.Update(ctx, agent)).To(Succeed())
	Expect(k8sClient.Delete(ctx, agent)).To(Succeed())
}
