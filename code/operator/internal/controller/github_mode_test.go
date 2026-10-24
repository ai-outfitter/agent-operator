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

// Independent org controllers must converge byte-for-byte: credential bytes feed
// the runtime rollout hash, so moving principals causes endless Recreate rollouts.
func TestGatewayCredentialMultiOrgConvergence(t *testing.T) {
	const first = "forge-gateway:first-org"
	const second = "forge-gateway:second-org"
	raw := []byte(`{"credentials":[{"principal":"voice-preview","token":"voice-kept"},{"principal":"forge-gateway:first-org","token":"first-kept","extra":"stable-extra"},{"principal":"forge-gateway:second-org","token":"second-kept"}],"other":"stable-config"}`)
	var err error
	raw, err = mergeGatewayCredential(raw, first, "first-kept")
	if err != nil {
		t.Fatal(err)
	}
	stable := string(raw)
	for i := range 20 {
		for _, item := range []struct{ principal, token string }{{second, "second-kept"}, {first, "first-kept"}} {
			raw, err = mergeGatewayCredential(raw, item.principal, item.token)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != stable {
				t.Fatalf("unchanged multi-org reconcile changed rollout bytes on iteration %d: %s", i, raw)
			}
		}
	}
	rotated, err := mergeGatewayCredential(raw, first, "rotated")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Credentials []struct{ Principal, Token, Extra string }
		Other       string
	}
	if err := json.Unmarshal(rotated, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Credentials) != 3 || parsed.Credentials[1].Principal != first || parsed.Credentials[1].Token != "rotated" || parsed.Credentials[1].Extra != "stable-extra" || parsed.Credentials[0].Token != "voice-kept" || parsed.Credentials[2].Token != "second-kept" || parsed.Other != "stable-config" {
		t.Fatalf("rotation changed position or unrelated authority: %s", rotated)
	}
	repeated, err := mergeGatewayCredential(rotated, first, "rotated")
	if err != nil || string(repeated) != string(rotated) {
		t.Fatalf("rotation not stable: %s %v", repeated, err)
	}
}
