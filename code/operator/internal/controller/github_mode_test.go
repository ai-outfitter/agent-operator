package controller

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const (
	testPollingChannels = "github,slack"
	testGitHubSource    = githubChannelSource
	testSlackSource     = "slack"
)

func TestWebhookModeOverridesLegacyPollingSelection(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "github-mode-fixture", Namespace: agentNamespace("luce")}, Data: map[string]string{OutfitterChannelsEnv: testPollingChannels}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
	r := &AgentReconciler{Client: c}
	org := &aioutfitterv1alpha1.Organization{Spec: aioutfitterv1alpha1.OrganizationSpec{AgentCatalogs: []aioutfitterv1alpha1.AgentCatalog{{GitHub: ptr.To("Unsupervisedcom/.agents")}}}}
	agent := &aioutfitterv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "luce"}}
	agent.Spec.EnvFrom = []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "github-mode-fixture"}}}}
	agent.Spec.GitHub = &aioutfitterv1alpha1.GitHubSpec{Mode: githubWebhookMode}
	agent.Spec.Channels = []string{testSlackSource}
	for _, want := range []string{testSlackSource, "off"} {
		values, err := r.runtimeConfigurationEnvironment(context.Background(), agent, org)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, v := range values {
			if v.Name == OutfitterChannelsEnv {
				got = v.Value
			}
		}
		if got != want {
			t.Fatalf("channels=%q want=%q", got, want)
		}
		agent.Spec.Channels = nil
	}
	agent.Spec.Channels = []string{testGitHubSource}
	if _, err := r.runtimeConfigurationEnvironment(context.Background(), agent, org); err == nil {
		t.Fatal("webhook mode accepted polling source")
	}
	// Polling mode preserves the previous environment override contract.
	agent.Spec.GitHub.Mode = "polling"
	values, err := r.runtimeConfigurationEnvironment(context.Background(), agent, org)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range values {
		if v.Name == OutfitterChannelsEnv {
			t.Fatal("polling mode overwrote legacy selection")
		}
	}
}

func TestGatewayCredentialPreservesOtherClients(t *testing.T) {
	raw := []byte(`{"credentials":[{"principal":"workflow","token":"keep","extra":"retained"},{"principal":"forge-gateway:unsupervised","token":"old"}],"other":"preserved"}`)
	merged, err := mergeGatewayCredential(raw, "forge-gateway:unsupervised", "new")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Credentials []struct{ Principal, Token, Extra string }
		Other       string
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Credentials) != 2 || got.Credentials[0].Token != "keep" || got.Credentials[0].Extra != "retained" || got.Credentials[1].Token != "new" || got.Other != "preserved" {
		t.Fatalf("configuration changed unexpectedly: %s", merged)
	}
	if _, err := mergeGatewayCredential([]byte(`broken`), "gateway", "new"); err == nil {
		t.Fatal("invalid existing config accepted")
	}
}
