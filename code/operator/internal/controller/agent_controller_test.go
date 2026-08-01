package controller

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

var _ = Describe("Agent Controller", func() {
	ctx := context.Background()

	It("reconciles the workload while reporting missing referenced objects", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("researcher"), organization.Name)
		secretName := "model-credentials"
		configName := "runtime-config"
		agent.Spec.Credentials = []aioutfitterv1alpha1.CredentialReference{
			{Secret: &secretName, As: aioutfitterv1alpha1.CredentialExposureEnv},
			{ConfigMap: &configName, As: aioutfitterv1alpha1.CredentialExposureVolume},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client:            k8sClient,
			APIReader:         k8sClient,
			Scheme:            k8sClient.Scheme(),
			AgentImage:        "example.test/agent-runtime@sha256:" + strings.Repeat("a", 64),
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

		actual := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		credentialsReady := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionCredentialsReady)
		Expect(credentialsReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(credentialsReady.Message).To(ContainSubstring(credentialKindSecret + "/model-credentials"))
		Expect(credentialsReady.Message).To(ContainSubstring(credentialKindConfig + "/runtime-config"))
		workloadReady := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionWorkloadReady)
		Expect(workloadReady.Status).To(Equal(metav1.ConditionFalse))
		Expect(workloadReady.Reason).To(Equal("DeploymentUnavailable"))
		Expect(apiMeta.IsStatusConditionFalse(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionReady)).To(BeTrue())

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].EnvFrom).To(ContainElement(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			},
		}))
		var configVolume *corev1.Volume
		for index := range deployment.Spec.Template.Spec.Volumes {
			if deployment.Spec.Template.Spec.Volumes[index].Name == credentialVolumeName(credentialKindConfig, configName) {
				configVolume = &deployment.Spec.Template.Spec.Volumes[index]
				break
			}
		}
		Expect(configVolume).NotTo(BeNil())
		Expect(configVolume.ConfigMap).NotTo(BeNil())
		Expect(configVolume.ConfigMap.Name).To(Equal(configName))
		Expect(configVolume.ConfigMap.Optional).To(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(reconciler.AgentImage))
	})

	It("rolls out a new user-owned runtime image", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("runtime-image"), organization.Name)
		agent.Spec.Image = "example.test/user-owned-agent:v1"
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), AgentImage: "agent-runtime:default",
		}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		deploymentKey := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: RuntimeName}
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(agent.Spec.Image))

		current := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, current)).To(Succeed())
		current.Spec.Image = "example.test/user-owned-agent:v2"
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(current.Spec.Image))
		Expect(deployment.Spec.Template.Spec.InitContainers[0].Image).To(Equal(current.Spec.Image))
	})

	It("projects references and becomes ready after the agent runtime starts", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("researcher"), organization.Name)
		agent.Spec.Image = "example.test/user-owned-agent:v1"
		secretName := "model-credentials"
		configName := "runtime-config"
		agent.Spec.Credentials = []aioutfitterv1alpha1.CredentialReference{
			{Secret: &secretName, As: aioutfitterv1alpha1.CredentialExposureEnv},
			{ConfigMap: &configName, As: aioutfitterv1alpha1.CredentialExposureVolume},
		}
		agent.Spec.Setup = []aioutfitterv1alpha1.SetupStep{
			{Name: "wait-for-mail", Script: "echo mail-ready"},
			{Name: "mail-bootstrap", Script: "echo setup-ready"},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), AgentImage: "agent-runtime:test",
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
		Expect(container.Image).To(Equal(agent.Spec.Image))
		Expect(deployment.Spec.Template.Spec.SecurityContext.RunAsUser).To(PointTo(Equal(int64(1000))))
		Expect(deployment.Spec.Template.Spec.SecurityContext.RunAsGroup).To(PointTo(Equal(int64(1000))))
		Expect(container.Command).To(BeEmpty())
		Expect(container.Args).To(BeEmpty())
		Expect(container.Env).To(ContainElements(
			corev1.EnvVar{Name: "AGENT_SLUG", Value: "researcher"},
			corev1.EnvVar{Name: "AGENT_HARNESS", Value: "pi"},
		))
		Expect(container.ReadinessProbe).To(BeNil())
		Expect(container.EnvFrom).To(ContainElement(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: "config-runtime-config", MountPath: CredentialsRoot + "/configmaps/runtime-config", ReadOnly: true,
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: SettingsName, MountPath: WorkspaceMount + "/.agents", ReadOnly: true,
		}))
		Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(3))
		for _, initContainer := range deployment.Spec.Template.Spec.InitContainers {
			Expect(initContainer.Image).To(Equal(agent.Spec.Image))
		}
		Expect(deployment.Spec.Template.Spec.InitContainers[1].Name).To(Equal("setup-wait-for-mail"))
		Expect(deployment.Spec.Template.Spec.InitContainers[1].Command).To(Equal([]string{"sh", "-c", "echo mail-ready"}))
		Expect(deployment.Spec.Template.Spec.InitContainers[2].Name).To(Equal("setup-mail-bootstrap"))
		Expect(deployment.Spec.Template.Spec.InitContainers).To(ContainElement(MatchFields(IgnoreExtras, Fields{
			"Name":    Equal("setup-mail-bootstrap"),
			"Image":   Equal(agent.Spec.Image),
			"Command": Equal([]string{"sh", "-c", "echo setup-ready"}),
			"Env": ContainElement(corev1.EnvVar{
				Name: HomeEnvName, Value: WorkspaceMount,
			}),
			"EnvFrom": ContainElement(corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
			}),
			"VolumeMounts": ContainElements(
				corev1.VolumeMount{Name: WorkspaceName, MountPath: WorkspaceMount},
				corev1.VolumeMount{Name: NixStoreName, MountPath: NixMount},
				corev1.VolumeMount{
					Name: "config-runtime-config", MountPath: CredentialsRoot + "/configmaps/runtime-config", ReadOnly: true,
				},
			),
		})))

		actual := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		settingsReady := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionOutfitterSettingsReady)
		Expect(settingsReady.Status).To(Equal(metav1.ConditionTrue))
		Expect(settingsReady.Reason).To(Equal("Ready"))

		deployment.Status.Replicas = 1
		deployment.Status.ReadyReplicas = 1
		deployment.Status.AvailableReplicas = 1
		Expect(k8sClient.Status().Update(ctx, deployment)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionOutfitterSettingsReady)).To(BeTrue())
		Expect(apiMeta.IsStatusConditionTrue(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionReady)).To(BeTrue())
	})

	It("rejects storage budgets that cannot hold both persistent claims", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("storage-budget"), organization.Name)
		agent.Spec.Workspace.ResourceQuota.Hard[corev1.ResourceRequestsStorage] = resource.MustParse("29Gi")
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}})
		Expect(err).NotTo(HaveOccurred())

		actual := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, actual)).To(Succeed())
		accepted := apiMeta.FindStatusCondition(actual.Status.Conditions, aioutfitterv1alpha1.AgentConditionAccepted)
		Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
		Expect(accepted.Reason).To(Equal("InvalidSpecification"))
		Expect(accepted.Message).To(ContainSubstring("workspace 10Gi + Nix store 20Gi"))
		namespace := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agentNamespace(agent.Name)}, namespace)).To(Satisfy(apierrors.IsNotFound))
	})

	It("merges a new image closure without overwriting persisted Nix paths", func() {
		root := GinkgoT().TempDir()
		imageOne := filepath.Join(root, "image-one")
		imageTwo := filepath.Join(root, "image-two")
		persistent := filepath.Join(root, "persistent")
		for _, directory := range []string{
			filepath.Join(imageOne, "store", "runtime-v1"),
			filepath.Join(imageTwo, "store", "runtime-v2"),
			filepath.Join(persistent, "store", "agent-installed"),
		} {
			Expect(os.MkdirAll(directory, 0o755)).To(Succeed())
		}
		Expect(os.WriteFile(filepath.Join(imageOne, "store", "runtime-v1", "version"), []byte("v1"), 0o644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(imageTwo, "store", "runtime-v2", "version"), []byte("v2"), 0o644)).To(Succeed())
		persistedPath := filepath.Join(persistent, "store", "agent-installed", "state")
		Expect(os.WriteFile(persistedPath, []byte("keep-me"), 0o644)).To(Succeed())

		seed := func(source string) {
			command := exec.Command("sh", "-c", nixStoreSeedScript)
			command.Env = append(os.Environ(), "SOURCE_NIX="+source, "DESTINATION_NIX="+persistent)
			output, err := command.CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(output))
		}
		seed(imageOne)
		Expect(os.WriteFile(filepath.Join(persistent, ".seeded"), []byte("old-image"), 0o644)).To(Succeed())
		seed(imageTwo)

		Expect(filepath.Join(persistent, "store", "runtime-v1", "version")).To(BeAnExistingFile())
		Expect(filepath.Join(persistent, "store", "runtime-v2", "version")).To(BeAnExistingFile())
		contents, err := os.ReadFile(persistedPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(contents)).To(Equal("keep-me"))
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

func createAcceptedOrganization(ctx context.Context) *aioutfitterv1alpha1.Organization {
	name := uniqueTestName("organization")
	revision := testCatalogRevision
	github := testCatalogGitHub
	organization := &aioutfitterv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aioutfitterv1alpha1.OrganizationSpec{AgentCatalogs: []aioutfitterv1alpha1.AgentCatalog{{
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

func validAgent(name, organization string) *aioutfitterv1alpha1.Agent {
	return &aioutfitterv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aioutfitterv1alpha1.AgentSpec{
			Memberships: []aioutfitterv1alpha1.Membership{{Organization: organization}},
			Profile:     aioutfitterv1alpha1.AgentProfile{Agent: "researcher", Harness: "pi"},
			Workspace: aioutfitterv1alpha1.WorkspaceSpec{
				ResourceQuota: aioutfitterv1alpha1.ResourceQuotaSpec{Hard: corev1.ResourceList{
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
				LimitRange: aioutfitterv1alpha1.WorkspaceLimitRangeSpec{Container: aioutfitterv1alpha1.ContainerLimitSpec{
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
	agent := &aioutfitterv1alpha1.Agent{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, agent)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	agent.Finalizers = nil
	Expect(k8sClient.Update(ctx, agent)).To(Succeed())
	Expect(k8sClient.Delete(ctx, agent)).To(Succeed())
}
