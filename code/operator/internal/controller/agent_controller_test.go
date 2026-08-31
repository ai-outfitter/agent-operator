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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const (
	researcherAgentSlug        = "researcher"
	testRuntimeConfigName      = "runtime-config"
	testRuntimeConfigMountPath = "/var/run/agent/inputs/runtime-config"
	testInputConfigName        = "config"
)

var _ = Describe("Agent Controller", func() {
	ctx := context.Background()

	It("inherits Organization defaults while preserving Agent overrides", func() {
		organization := createAcceptedOrganization(ctx)
		organizationCredentials := &corev1.Secret{}
		organizationKey := types.NamespacedName{Namespace: organizationNamespace(organization.Name), Name: defaultOrgCredentials}
		Expect(k8sClient.Get(ctx, organizationKey, organizationCredentials)).To(Succeed())
		organizationCredentials.Data = map[string][]byte{
			"default.SPARK_AUTHORIZATION": []byte("spark-v1"),
			"default.OPENAI_BASE_URL":     []byte("https://models.example.test/v1"),
			forgeWebhookSecretKey:         []byte("must-not-inherit"),
		}
		Expect(k8sClient.Update(ctx, organizationCredentials)).To(Succeed())

		agent := validAgent(uniqueTestName(researcherAgentSlug), organization.Name)
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)
		namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: agentNamespace(agent.Name)}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, namespace) })
		agentCredentials := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultAgentCredentials, Namespace: namespace.Name}, Data: map[string][]byte{
			"OPENAI_BASE_URL": []byte("https://agent.example.test/v1"),
		}}
		Expect(k8sClient.Create(ctx, agentCredentials)).To(Succeed())

		reconciler := &AgentReconciler{Client: k8sClient}
		missing, err := reconciler.ensureStandardAgentCredentials(ctx, agent, organization)
		Expect(err).NotTo(HaveOccurred())
		Expect(missing).To(BeEmpty())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace.Name, Name: defaultAgentCredentials}, agentCredentials)).To(Succeed())
		Expect(agentCredentials.Data).To(HaveKeyWithValue("SPARK_AUTHORIZATION", []byte("spark-v1")))
		Expect(agentCredentials.Data).To(HaveKeyWithValue("OPENAI_BASE_URL", []byte("https://agent.example.test/v1")))
		Expect(agentCredentials.Data).NotTo(HaveKey(forgeWebhookSecretKey))

		Expect(k8sClient.Get(ctx, organizationKey, organizationCredentials)).To(Succeed())
		organizationCredentials.Data["default.SPARK_AUTHORIZATION"] = []byte("spark-v2")
		Expect(k8sClient.Update(ctx, organizationCredentials)).To(Succeed())
		_, err = reconciler.ensureStandardAgentCredentials(ctx, agent, organization)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace.Name, Name: defaultAgentCredentials}, agentCredentials)).To(Succeed())
		Expect(agentCredentials.Data).To(HaveKeyWithValue("SPARK_AUTHORIZATION", []byte("spark-v2")))

		agentCredentials.Data["SPARK_AUTHORIZATION"] = []byte("agent-spark")
		Expect(k8sClient.Update(ctx, agentCredentials)).To(Succeed())
		Expect(k8sClient.Get(ctx, organizationKey, organizationCredentials)).To(Succeed())
		organizationCredentials.Data["default.SPARK_AUTHORIZATION"] = []byte("spark-v3")
		Expect(k8sClient.Update(ctx, organizationCredentials)).To(Succeed())
		_, err = reconciler.ensureStandardAgentCredentials(ctx, agent, organization)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace.Name, Name: defaultAgentCredentials}, agentCredentials)).To(Succeed())
		Expect(agentCredentials.Data).To(HaveKeyWithValue("SPARK_AUTHORIZATION", []byte("agent-spark")))

		delete(agentCredentials.Data, "SPARK_AUTHORIZATION")
		Expect(k8sClient.Update(ctx, agentCredentials)).To(Succeed())
		_, err = reconciler.ensureStandardAgentCredentials(ctx, agent, organization)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace.Name, Name: defaultAgentCredentials}, agentCredentials)).To(Succeed())
		Expect(agentCredentials.Data).To(HaveKeyWithValue("SPARK_AUTHORIZATION", []byte("spark-v3")))
	})

	It("rejects unsafe generalized input projections", func() {
		agent := &aioutfitterv1alpha1.Agent{Spec: aioutfitterv1alpha1.AgentSpec{
			Volumes:      []corev1.Volume{{Name: "host", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}}},
			VolumeMounts: []corev1.VolumeMount{{Name: "host", MountPath: "/host", ReadOnly: true}},
		}}
		Expect(inputValidationMessage(agent)).To(ContainSubstring("exactly one Secret or ConfigMap"))
		agent.Spec.Volumes = []corev1.Volume{{Name: testInputConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: testInputConfigName}}}}}
		agent.Spec.VolumeMounts = []corev1.VolumeMount{{Name: testInputConfigName, MountPath: WorkspaceMount + "/config", ReadOnly: true}}
		Expect(inputValidationMessage(agent)).To(ContainSubstring("overlaps reserved path"))
	})

	It("reconciles the workload while reporting missing referenced objects", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName(researcherAgentSlug), organization.Name)
		agent.Spec.Channels = []string{"slack", "github"}
		secretName := "model-credentials"
		configName := testRuntimeConfigName
		legacyConfigName := "legacy-github-notify"
		agent.Spec.EnvFrom = []corev1.EnvFromSource{
			{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}}},
			{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: legacyConfigName}}},
		}
		agent.Spec.Volumes = []corev1.Volume{{Name: testRuntimeConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName}}}}}
		agent.Spec.VolumeMounts = []corev1.VolumeMount{{Name: testRuntimeConfigName, MountPath: testRuntimeConfigMountPath, ReadOnly: true}}
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
			if deployment.Spec.Template.Spec.Volumes[index].Name == testRuntimeConfigName {
				configVolume = &deployment.Spec.Template.Spec.Volumes[index]
				break
			}
		}
		Expect(configVolume).NotTo(BeNil())
		Expect(configVolume.ConfigMap).NotTo(BeNil())
		Expect(configVolume.ConfigMap.Name).To(Equal(configName))
		Expect(configVolume.ConfigMap.Optional).To(BeNil())
		Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(reconciler.AgentImage))

		// The relay client's identity must survive pod replacement, so it is
		// operator-projected rather than derived in the runtime. Renaming or
		// dropping one of these silently breaks agent-to-agent addressing and
		// strands the local spool, with no error at startup.
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
			corev1.EnvVar{Name: "AGENT_ENDPOINT_ID", Value: "link:" + agent.Name},
			corev1.EnvVar{Name: "AGENT_PRINCIPAL_ID", Value: "link:" + agent.Name},
			corev1.EnvVar{Name: "AGENT_SPOOL_PATH", Value: "/workspace/.channels/agent"},
			corev1.EnvVar{Name: OutfitterChannelsEnv, Value: "github,slack"},
			corev1.EnvVar{Name: GitHubNotifyOrgsEnv, Value: testOutfitterOrg},
			corev1.EnvVar{Name: GitHubNotifyPollMSEnv, Value: "60000"},
			corev1.EnvVar{Name: GitHubNotifyFiltersEnv, Value: DefaultGitHubFilters},
		))

		// Existing env-projected values win during migration: once the referenced
		// Secret appears, the operator stops emitting explicit values that would
		// otherwise override EnvFrom.
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespaceName},
			StringData: map[string]string{
				GitHubNotifyOrgsEnv: "legacy-org", GitHubNotifyPollMSEnv: "30000",
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: legacyConfigName, Namespace: namespaceName},
			Data: map[string]string{
				OutfitterChannelsEnv: "legacy", GitHubNotifyFiltersEnv: "mention",
			},
		})).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}, deployment)).To(Succeed())
		for _, value := range deployment.Spec.Template.Spec.Containers[0].Env {
			Expect(value.Name).NotTo(BeElementOf(
				OutfitterChannelsEnv, GitHubNotifyOrgsEnv, GitHubNotifyPollMSEnv, GitHubNotifyFiltersEnv,
			))
		}

		// Agent-only pods must still get a usable API token: automount is off
		// pod-wide, replaced by an explicit projection into the agent
		// container at the well-known path.
		Expect(deployment.Spec.Template.Spec.AutomountServiceAccountToken).To(Equal(ptr.To(false)))
		Expect(deployment.Spec.Template.Spec.Containers[0].VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: APITokenVolumeName, MountPath: APITokenMountPath, ReadOnly: true,
		}))
		var tokenVolume *corev1.Volume
		for index := range deployment.Spec.Template.Spec.Volumes {
			if deployment.Spec.Template.Spec.Volumes[index].Name == APITokenVolumeName {
				tokenVolume = &deployment.Spec.Template.Spec.Volumes[index]
				break
			}
		}
		// Setup steps are user bootstrap and could always reach the API
		// server; turning automount off must not silently take that away.
		// seed-nix-store stays without a token — it only copies store paths.
		for _, initContainer := range deployment.Spec.Template.Spec.InitContainers {
			mountNames := []string{}
			for _, mount := range initContainer.VolumeMounts {
				mountNames = append(mountNames, mount.Name)
			}
			if strings.HasPrefix(initContainer.Name, "setup-") {
				Expect(mountNames).To(ContainElement(APITokenVolumeName),
					"setup init container %s lost API access", initContainer.Name)
			} else {
				Expect(mountNames).NotTo(ContainElement(APITokenVolumeName),
					"init container %s should not carry the API token", initContainer.Name)
			}
		}

		Expect(tokenVolume).NotTo(BeNil())
		Expect(tokenVolume.Projected).NotTo(BeNil())
		var hasTokenSource bool
		for _, source := range tokenVolume.Projected.Sources {
			if source.ServiceAccountToken != nil {
				hasTokenSource = true
				Expect(source.ServiceAccountToken.Path).To(Equal("token"))
			}
		}
		Expect(hasTokenSource).To(BeTrue())
	})

	It("projects Agent channel and GitHub notification overrides", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("github-notify"), organization.Name)
		agent.Spec.Channels = []string{"slack", "github"}
		pollMS := int64(15000)
		agent.Spec.GitHub = &aioutfitterv1alpha1.GitHubSpec{
			NotifyOrgs: []string{"example-one", "example-two"},
			PollMS:     &pollMS,
			Filters:    []string{"review_requested", "mention"},
		}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}})
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: RuntimeName}, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers[0].Env).To(ContainElements(
			corev1.EnvVar{Name: OutfitterChannelsEnv, Value: "github,slack"},
			corev1.EnvVar{Name: GitHubNotifyOrgsEnv, Value: "example-one,example-two"},
			corev1.EnvVar{Name: GitHubNotifyPollMSEnv, Value: "15000"},
			corev1.EnvVar{Name: GitHubNotifyFiltersEnv, Value: "mention,review_requested"},
		))
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

	It("adds a browser sidecar when spec.browser is enabled", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("browser"), organization.Name)
		agent.Spec.Browser = &aioutfitterv1alpha1.BrowserSpec{Enabled: true}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), AgentImage: "link-agent:default",
		}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		deployment := &appsv1.Deployment{}
		deploymentKey := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: RuntimeName}
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		containers := deployment.Spec.Template.Spec.Containers
		Expect(containers).To(HaveLen(2))
		Expect(containers[1].Name).To(Equal(BrowserName))
		Expect(containers[1].Image).To(Equal(defaultBrowserImage))
		// The Command must bypass the image entrypoint: the headless-shell
		// wrapper starts a socat forwarder on 0.0.0.0:9222, which would
		// expose the unauthenticated CDP listener outside the Pod.
		Expect(containers[1].Command).To(Equal([]string{"/headless-shell/headless-shell"}))
		Expect(containers[1].Args).To(ContainElement("--remote-debugging-address=127.0.0.1"))
		Expect(containers[1].Args).To(ContainElement("--remote-debugging-port=9222"))
		Expect(containers[1].Args).To(ContainElement("--no-sandbox"))

		// The unsandboxed browser must never hold the agent-runtime
		// ServiceAccount credentials: no pod-wide automount, no token mount
		// in the browser container — only the agent container gets the
		// projected token.
		Expect(deployment.Spec.Template.Spec.AutomountServiceAccountToken).To(Equal(ptr.To(false)))
		for _, mount := range containers[1].VolumeMounts {
			Expect(mount.Name).NotTo(Equal(APITokenVolumeName))
			Expect(mount.MountPath).NotTo(HavePrefix("/var/run/secrets/kubernetes.io"))
		}
		agentMountNames := make([]string, 0, len(containers[0].VolumeMounts))
		for _, mount := range containers[0].VolumeMounts {
			agentMountNames = append(agentMountNames, mount.Name)
		}
		Expect(agentMountNames).To(ContainElement(APITokenVolumeName))
		Expect(containers[0].Env).To(ContainElement(corev1.EnvVar{
			Name: BrowserCDPURLEnvName, Value: BrowserCDPURL,
		}))
		volumeNames := make([]string, 0, len(deployment.Spec.Template.Spec.Volumes))
		for _, volume := range deployment.Spec.Template.Spec.Volumes {
			volumeNames = append(volumeNames, volume.Name)
		}
		Expect(volumeNames).To(ContainElement(browserDataName))

		// Disabling the browser removes the sidecar again.
		current := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, current)).To(Succeed())
		current.Spec.Browser = nil
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
		Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
	})

	It("projects references and becomes ready after the agent runtime starts", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName(researcherAgentSlug), organization.Name)
		agent.Spec.Image = "example.test/user-owned-agent:v1"
		secretName := "model-credentials"
		configName := testRuntimeConfigName
		agent.Spec.EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}}}}
		agent.Spec.Volumes = []corev1.Volume{{Name: testRuntimeConfigName, VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName}}}}}
		agent.Spec.VolumeMounts = []corev1.VolumeMount{{Name: testRuntimeConfigName, MountPath: testRuntimeConfigMountPath, ReadOnly: true}}
		agent.Spec.CatalogSync = &aioutfitterv1alpha1.CatalogSyncSpec{Enabled: true}
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
		// The operator supplies the resident invocation rather than relying on a baked
		// entrypoint. That reliance is what forced this repository to publish its own agent
		// image: the stock Outfitter container's entrypoint is bare `outfitter`, which prints
		// usage and exits. Stdin keeps the RPC session alive between wakes.
		//
		// The session identity is the Agent CR name, not the profile slug: two Agents
		// sharing the `researcher` profile must not share a conversation, and the stable
		// id is what lets the durable JSONL transcript on the workspace PVC resume
		// across pod restarts.
		Expect(container.Args).To(Equal([]string{
			"run", researcherAgentSlug, "--strict", "--", "--mode", "rpc", "--session-id", agent.Name,
		}))
		Expect(container.Stdin).To(BeTrue())
		Expect(container.Env).To(ContainElements(
			corev1.EnvVar{Name: "AGENT_SLUG", Value: researcherAgentSlug},
			corev1.EnvVar{Name: "AGENT_HARNESS", Value: "pi"},
		))
		Expect(container.ReadinessProbe).To(BeNil())
		Expect(container.EnvFrom).To(ContainElement(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: testRuntimeConfigName, MountPath: testRuntimeConfigMountPath, ReadOnly: true,
		}))
		Expect(container.VolumeMounts).To(ContainElement(corev1.VolumeMount{
			Name: SettingsName, MountPath: WorkspaceMount + "/.agents", ReadOnly: true,
		}))
		Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(4))
		for _, initContainer := range deployment.Spec.Template.Spec.InitContainers {
			Expect(initContainer.Image).To(Equal(agent.Spec.Image))
		}
		Expect(deployment.Spec.Template.Spec.InitContainers[1].Name).To(Equal("sync-agent-catalog"))
		Expect(deployment.Spec.Template.Spec.InitContainers[1].Command).To(Equal([]string{"sh", "-c", catalogSyncScript}))
		Expect(deployment.Spec.Template.Spec.InitContainers[1].EnvFrom).To(ContainElement(corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: secretName}},
		}))
		Expect(deployment.Spec.Template.Spec.InitContainers[1].VolumeMounts).To(ContainElements(
			corev1.VolumeMount{Name: WorkspaceName, MountPath: WorkspaceMount},
			corev1.VolumeMount{Name: SettingsName, MountPath: WorkspaceMount + "/.agents", ReadOnly: true},
			corev1.VolumeMount{Name: NixStoreName, MountPath: NixMount},
		))
		Expect(deployment.Spec.Template.Spec.InitContainers[1].VolumeMounts).NotTo(ContainElement(
			corev1.VolumeMount{Name: APITokenVolumeName, MountPath: APITokenMountPath, ReadOnly: true},
		))
		Expect(deployment.Spec.Template.Spec.InitContainers[2].Name).To(Equal("setup-wait-for-mail"))
		Expect(deployment.Spec.Template.Spec.InitContainers[2].Command).To(Equal([]string{"sh", "-c", "echo mail-ready"}))
		Expect(deployment.Spec.Template.Spec.InitContainers[3].Name).To(Equal("setup-mail-bootstrap"))
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
					Name: testRuntimeConfigName, MountPath: testRuntimeConfigMountPath, ReadOnly: true,
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

	// The `-nix` tag suffix is the published convention for the Nix closure
	// variant; the plain published tag becomes a Debian base at 1.5.0. Anything
	// the operator cannot classify keeps the machinery, so existing closure
	// deployments (1.4.0 and earlier carry no suffix) and custom downstream
	// images keep working; the machinery disappears when a deployment moves to
	// >= 1.5.0 or a bare digest pin.
	DescribeTable("classifies which runtime images need the nix-store machinery",
		func(image string, expected bool) {
			Expect(imageNeedsNixStore(image)).To(Equal(expected))
		},
		Entry("published closure image before the suffix convention", "ghcr.io/ai-outfitter/outfitter:1.4.0", true),
		Entry("earlier published semver", "ghcr.io/ai-outfitter/outfitter:1.3.2", true),
		Entry("Debian primary tag", "ghcr.io/ai-outfitter/outfitter:1.5.0", false),
		Entry("later Debian release", "ghcr.io/ai-outfitter/outfitter:2.0.1", false),
		Entry("closure suffix", "ghcr.io/ai-outfitter/outfitter:1.5.0-nix", true),
		Entry("closure suffix on a digest-pinned reference",
			"ghcr.io/ai-outfitter/outfitter:1.5.0-nix@sha256:"+strings.Repeat("a", 64), true),
		Entry("Debian tag on a digest-pinned reference",
			"ghcr.io/ai-outfitter/outfitter:1.5.0@sha256:"+strings.Repeat("a", 64), false),
		Entry("bare digest reference with no tag",
			"example.test/agent-runtime@sha256:"+strings.Repeat("a", 64), false),
		Entry("custom downstream image tag fails safe", "ghcr.io/unsupervisedcom/research-agent:latest", true),
		Entry("dev tag fails safe", "agent-runtime:dev", true),
		Entry("registry port without a tag fails safe", "localhost:5000/outfitter", true),
		Entry("registry port with a Debian tag", "localhost:5000/outfitter:1.5.0", false),
		Entry("v-prefixed semver fails safe", "ghcr.io/ai-outfitter/outfitter:v1.5.0", true),
		Entry("pre-release tag fails safe", "ghcr.io/ai-outfitter/outfitter:1.5.0-rc.1", true),
	)

	It("gates the nix-store machinery on the runtime image", func() {
		organization := createAcceptedOrganization(ctx)
		agent := validAgent(uniqueTestName("nix-gate"), organization.Name)
		agent.Spec.Image = "ghcr.io/ai-outfitter/outfitter:1.5.0"
		agent.Spec.Setup = []aioutfitterv1alpha1.SetupStep{{Name: "bootstrap", Script: "echo ready"}}
		Expect(k8sClient.Create(ctx, agent)).To(Succeed())
		DeferCleanup(removeAgent, ctx, agent.Name)

		reconciler := &AgentReconciler{
			Client: k8sClient, APIReader: k8sClient, Scheme: k8sClient.Scheme(), AgentImage: "agent-runtime:default",
		}
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: agent.Name}}
		_, err := reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		namespaceName := agentNamespace(agent.Name)
		deploymentKey := types.NamespacedName{Namespace: namespaceName, Name: RuntimeName}
		nixClaimKey := types.NamespacedName{Namespace: namespaceName, Name: NixStoreName}
		deployment := &appsv1.Deployment{}
		expectNixMachinery := func(expected bool) {
			GinkgoHelper()
			Expect(k8sClient.Get(ctx, deploymentKey, deployment)).To(Succeed())
			initNames := make([]string, 0, len(deployment.Spec.Template.Spec.InitContainers))
			for _, initContainer := range deployment.Spec.Template.Spec.InitContainers {
				initNames = append(initNames, initContainer.Name)
				for _, mount := range initContainer.VolumeMounts {
					if !expected {
						Expect(mount.Name).NotTo(Equal(NixStoreName))
					}
					// seed-nix-store only copies store paths; it must never
					// carry the API token.
					if initContainer.Name == "seed-nix-store" {
						Expect(mount.Name).NotTo(Equal(APITokenVolumeName))
					}
				}
			}
			volumeNames := make([]string, 0, len(deployment.Spec.Template.Spec.Volumes))
			for _, volume := range deployment.Spec.Template.Spec.Volumes {
				volumeNames = append(volumeNames, volume.Name)
			}
			agentContainer := deployment.Spec.Template.Spec.Containers[0]
			mountNames := make([]string, 0, len(agentContainer.VolumeMounts))
			for _, mount := range agentContainer.VolumeMounts {
				mountNames = append(mountNames, mount.Name)
			}
			if expected {
				Expect(initNames).To(ContainElement("seed-nix-store"))
				Expect(volumeNames).To(ContainElement(NixStoreName))
				Expect(mountNames).To(ContainElement(NixStoreName))
			} else {
				Expect(initNames).NotTo(ContainElement("seed-nix-store"))
				Expect(volumeNames).NotTo(ContainElement(NixStoreName))
				Expect(mountNames).NotTo(ContainElement(NixStoreName))
			}
			// Setup init containers survive the gate either way.
			Expect(initNames).To(ContainElement("setup-bootstrap"))
		}

		// Debian primary tag: no seed init container, no /nix mounts, no nix
		// volume, and the nix-store PVC is never created.
		expectNixMachinery(false)
		claim := &corev1.PersistentVolumeClaim{}
		Expect(k8sClient.Get(ctx, nixClaimKey, claim)).To(Satisfy(apierrors.IsNotFound))

		// Switching to the `-nix` closure variant brings the machinery and the
		// PVC back.
		current := &aioutfitterv1alpha1.Agent{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, current)).To(Succeed())
		current.Spec.Image = "ghcr.io/ai-outfitter/outfitter:1.5.0-nix"
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		expectNixMachinery(true)
		Expect(k8sClient.Get(ctx, nixClaimKey, claim)).To(Succeed())

		// Switching back to a non-closure image stops mounting the store but
		// leaves the pre-existing PVC alone: deleting agent-installed Nix state
		// on an image switch would be an unrecoverable surprise.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: agent.Name}, current)).To(Succeed())
		current.Spec.Image = "ghcr.io/ai-outfitter/outfitter:1.5.0"
		Expect(k8sClient.Update(ctx, current)).To(Succeed())
		_, err = reconciler.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		expectNixMachinery(false)
		Expect(k8sClient.Get(ctx, nixClaimKey, claim)).To(Succeed())
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
	Expect(k8sClient.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name: defaultOrgCredentials, Namespace: organizationNamespace(name),
	}})).To(Succeed())
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, organization)).To(Succeed())
	DeferCleanup(removeOrganization, ctx, name)
	return organization
}

func validAgent(name, organization string) *aioutfitterv1alpha1.Agent {
	return &aioutfitterv1alpha1.Agent{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: aioutfitterv1alpha1.AgentSpec{
			Memberships: []aioutfitterv1alpha1.Membership{{Organization: organization}},
			Profile:     aioutfitterv1alpha1.AgentProfile{Agent: researcherAgentSlug, Harness: "pi"},
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
