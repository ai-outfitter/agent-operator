package controller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDirectPiWithoutCatalog(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, aioutfitterv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	org := &aioutfitterv1alpha1.Organization{ObjectMeta: metav1.ObjectMeta{Name: "pi-org"}}
	if message := validateOrganization(org); message != "" {
		t.Fatal(message)
	}
	agent := validAgent("pi-agent", org.Name)
	agent.Spec.Profile = aioutfitterv1alpha1.AgentProfile{}
	agent.Spec.Pi = &aioutfitterv1alpha1.PiSpec{SystemPrompt: "Answer addressed questions; implement assigned issues.", Model: "provider/model", Packages: []string{"/opt/pi/node_modules/@ai-outfitter/channels"}}
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	r := &AgentReconciler{Client: c, Scheme: scheme, AgentImage: "pi:test"}
	data, err := r.renderSettingsData(agent, org)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := data[SettingsFile]; found {
		t.Fatal("rendered Outfitter catalog settings")
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(data["settings.json"]), &settings); err != nil {
		t.Fatal(err)
	}
	if settings["defaultProvider"] != "provider" || settings["defaultModel"] != "model" {
		t.Fatal(settings)
	}
	deployment, err := r.ensureAgentDeployment(ctx, agent, org)
	if err != nil {
		t.Fatal(err)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if len(container.Command) != 1 || container.Command[0] != "pi" {
		t.Fatal("not invoking Pi directly", container.Command)
	}
	for _, init := range deployment.Spec.Template.Spec.InitContainers {
		if strings.Contains(strings.Join(init.Command, " "), "outfitter sync") || init.Name == catalogSyncContainerName || init.Name == workflowExportContainerName {
			t.Fatal("catalog startup retained", init.Name)
		}
	}
	for _, mount := range container.VolumeMounts {
		if strings.HasSuffix(mount.MountPath, "/.agents") {
			t.Fatal("catalog mount retained")
		}
	}
	before := deployment.Spec.Template.Annotations[SettingsHashAnnotation]
	agent.Spec.Pi.SystemPrompt = "A revised instruction"
	if _, err := r.ensureAgentDeployment(ctx, agent, org); err != nil {
		t.Fatal(err)
	}
	got := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: RuntimeName}, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.Template.Annotations[SettingsHashAnnotation] == before {
		t.Fatal("prompt update did not trigger rollout")
	}
}
