package controller

import (
	"context"
	"strings"
	"testing"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNativeTaskPlanePreservesResidentConfiguration(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "catalog"
		if direct {
			name = "direct-pi"
		}
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, aioutfitterv1alpha1.AddToScheme} {
				if err := add(scheme); err != nil {
					t.Fatal(err)
				}
			}
			org := &aioutfitterv1alpha1.Organization{ObjectMeta: metav1.ObjectMeta{Name: "example"}}
			agent := validAgent("resident", org.Name)
			agent.Spec.Profile.Model = "provider/preserve-me"
			if direct {
				agent.Spec.Profile = aioutfitterv1alpha1.AgentProfile{}
				agent.Spec.Pi = &aioutfitterv1alpha1.PiSpec{SystemPrompt: "Preserve these instructions.", Model: "provider/preserve-me", Packages: []string{"/opt/pi/node_modules/@ai-outfitter/channels"}}
			}
			r := &AgentReconciler{Client: fake.NewClientBuilder().WithScheme(scheme).Build(), Scheme: scheme, AgentImage: testAgentImage}
			baseline, err := r.ensureAgentDeployment(context.Background(), agent, org)
			if err != nil {
				t.Fatal(err)
			}
			agent.Spec.TaskPlane = &aioutfitterv1alpha1.AgentTaskPlaneSpec{Enabled: true}
			agent.Spec.ManagedMcp = &aioutfitterv1alpha1.ManagedMcpSpec{SecretName: testManagedMcpSecret}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: testManagedMcpSecret, Namespace: agentNamespace(agent.Name)}, Data: map[string][]byte{"mcp.json": []byte(`{"mcpServers":{"managed":{"url":"https://example.test"}}}`), testManagedMcpToken: []byte("first")}}
			if err := r.Create(context.Background(), secret); err != nil {
				t.Fatal(err)
			}
			deployment, err := r.ensureAgentDeployment(context.Background(), agent, org)
			if err != nil {
				t.Fatal(err)
			}
			container := deployment.Spec.Template.Spec.Containers[0]
			if strings.Join(container.Command, " ") != strings.Join(baseline.Spec.Template.Spec.Containers[0].Command, " ") || strings.Join(container.Args, " ") != strings.Join(baseline.Spec.Template.Spec.Containers[0].Args, " ") {
				t.Fatal("task intake changed the resident invocation")
			}
			foundServer, foundCredentials := false, false
			for _, env := range container.Env {
				if env.Name == A2AWorkflowEnv || env.Name == A2AWorkflowManifestEnv {
					t.Fatal("generic native intake unexpectedly selects a workflow", env.Name)
				}
				foundServer = foundServer || (env.Name == a2aServerEnv && env.Value == "1")
				foundCredentials = foundCredentials || (env.Name == "A2A_CREDENTIALS_PATH" && env.Value == A2ACredentialsMount+"/credentials.json")
			}
			assertManagedMcpProjection(t, direct, deployment)
			oldHash := deployment.Spec.Template.Annotations[credentialHashAnnotation]
			secret.Data[testManagedMcpToken] = []byte("rotated")
			if err := r.Update(context.Background(), secret); err != nil {
				t.Fatal(err)
			}
			rotated, err := r.ensureAgentDeployment(context.Background(), agent, org)
			if err != nil {
				t.Fatal(err)
			}
			if rotated.Spec.Template.Annotations[credentialHashAnnotation] == oldHash {
				t.Fatal("managed MCP credential rotation did not roll resident")
			}
			if !foundServer || !foundCredentials {
				t.Fatal("native intake missing authenticated A2A environment")
			}
			if len(container.Ports) != 1 || container.Ports[0].ContainerPort != A2APort {
				t.Fatal("native intake port is missing")
			}
			for _, init := range deployment.Spec.Template.Spec.InitContainers {
				if init.Name == workflowExportContainerName {
					t.Fatal("generic task plane exported a workflow")
				}
			}
			settings, err := r.renderSettingsData(agent, org)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(settings[SettingsFile], "workflows:") {
				t.Fatal("generic task plane configured a catalog workflow")
			}
			assertNativeTaskCredentials(t, agent, deployment)
		})
	}
}

func assertManagedMcpProjection(t *testing.T, direct bool, deployment *appsv1.Deployment) {
	t.Helper()
	container := deployment.Spec.Template.Spec.Containers[0]
	foundManagedConfig, foundCredential := false, false
	for _, env := range container.Env {
		foundManagedConfig = foundManagedConfig || env.Name == managedMcpConfigEnv
		foundCredential = foundCredential || (env.Name == testManagedMcpToken && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil)
		if env.Name == managedMcpConfigKey {
			t.Fatal("MCP document must not be exported as environment")
		}
	}
	if foundManagedConfig == direct || !foundCredential {
		t.Fatal("incorrect managed configuration or credential projection")
	}
	for _, source := range container.EnvFrom {
		if source.SecretRef != nil && source.SecretRef.Name == testManagedMcpSecret {
			t.Fatal("managed Secret must not be envFrom")
		}
	}
	for _, init := range deployment.Spec.Template.Spec.InitContainers {
		if init.Name == "merge-managed-mcp" {
			t.Fatal("redundant native merge init container retained")
		}
		if init.Name == copySettingsContainerName && direct && !strings.Contains(init.Command[2], "cp -L "+managedMcpMount) {
			t.Fatal("direct MCP configuration not copied into runtime settings")
		}
	}
}

func assertNativeTaskCredentials(t *testing.T, agent *aioutfitterv1alpha1.Agent, deployment *appsv1.Deployment) {
	t.Helper()
	foundVolume := false
	for _, volume := range deployment.Spec.Template.Spec.Volumes {
		if volume.Name == A2ACredentialsVolumeName {
			foundVolume = volume.Secret != nil && volume.Secret.SecretName == agentCredentialSecretName(agent) && len(volume.Secret.Items) == 1 && volume.Secret.Items[0].Key == agentA2ACredentialsKey
		}
	}
	if !foundVolume {
		t.Fatal("native intake is not using the existing resident credential secret")
	}
}
