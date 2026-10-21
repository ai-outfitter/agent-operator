package controller

import (
	"context"
	"encoding/json"
	"testing"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestTransportCredentialRotationRollsGateway(t *testing.T) {
	const rotationOrg = "rotation"
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, appsv1.AddToScheme, networkingv1.AddToScheme, aioutfitterv1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	org := &aioutfitterv1alpha1.Organization{ObjectMeta: metav1.ObjectMeta{Name: rotationOrg, UID: rotationOrg}, Spec: aioutfitterv1alpha1.OrganizationSpec{Forge: &aioutfitterv1alpha1.OrganizationForgeSpec{Provider: testGitHubSource, Owner: rotationOrg}}}
	ns := organizationNamespace(org.Name)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: defaultOrgCredentials, Namespace: ns}, Data: map[string][]byte{forgeRoutesKey: []byte("[]"), forgeWebhookSecretKey: []byte("initial")}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()
	r := &OrganizationReconciler{Client: c, Scheme: scheme, GatewayImage: "gateway:test"}
	readHash := func() string {
		t.Helper()
		if err := r.ensureGatewayResources(ctx, org, nil); err != nil {
			t.Fatal(err)
		}
		deployment := &appsv1.Deployment{}
		if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: ForgeGatewayName}, deployment); err != nil {
			t.Fatal(err)
		}
		if deployment.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType || deployment.Spec.Strategy.RollingUpdate != nil {
			t.Fatal("gateway must use single-writer recreate rollout")
		}
		return deployment.Spec.Template.Annotations[credentialHashAnnotation]
	}
	initial := readHash()
	if initial == "" {
		t.Fatal("missing rollout hash")
	}
	secret.Data["unrelated"] = []byte("changed")
	if err := c.Update(ctx, secret); err != nil {
		t.Fatal(err)
	}
	if readHash() != initial {
		t.Fatal("unrelated credential triggered rollout")
	}
	for _, key := range []string{forgeWebhookSecretKey, forgeRoutesKey} {
		secret.Data[key] = []byte("rotated")
		if err := c.Update(ctx, secret); err != nil {
			t.Fatal(err)
		}
		next := readHash()
		if next == initial {
			t.Fatalf("%s rotation did not roll gateway", key)
		}
		initial = next
	}
	agent := &aioutfitterv1alpha1.Agent{ObjectMeta: metav1.ObjectMeta{Name: "rotation-agent"}}
	agentNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: agentNamespace(agent.Name)}}
	if err := c.Create(ctx, agentNS); err != nil {
		t.Fatal(err)
	}
	if err := r.ensureAgentA2A(ctx, org, agent, "first", map[string]string{OrganizationLabel: org.Name}); err != nil {
		t.Fatal(err)
	}
	old, err := transportCredentialHash(ctx, c, agentNS.Name, defaultAgentCredentials, agentA2ACredentialsKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ensureAgentA2A(ctx, org, agent, "second", map[string]string{OrganizationLabel: org.Name}); err != nil {
		t.Fatal(err)
	}
	next, err := transportCredentialHash(ctx, c, agentNS.Name, defaultAgentCredentials, agentA2ACredentialsKey)
	if err != nil || next == old {
		t.Fatal("agent credential rotation did not change rollout hash", err)
	}
	agentSecret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: agentNS.Name, Name: defaultAgentCredentials}, agentSecret); err != nil {
		t.Fatal(err)
	}
	ar := &AgentReconciler{Client: c}
	requests := ar.mapManagedResource(ctx, agentSecret)
	if len(requests) != 1 || requests[0].Name != agent.Name {
		t.Fatal("credential update does not enqueue its resident")
	}
	bad := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "other-agent", Labels: map[string]string{AgentNameLabel: "other"}}, Data: map[string][]byte{agentA2ACredentialsKey: []byte("broken")}}
	if err := c.Create(ctx, bad); err != nil {
		t.Fatal(err)
	}
	if _, err := removeGatewayCredential([]byte(`{"other":"keep"}`), "unused"); err != nil {
		t.Fatal(err)
	}
	if err := r.cleanupStaleAgentA2A(ctx, org, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Namespace: agentNS.Name, Name: defaultAgentCredentials}, agentSecret); err != nil {
		t.Fatal(err)
	}
	var revoked struct {
		Credentials []any `json:"credentials"`
	}
	if err := json.Unmarshal(agentSecret.Data[agentA2ACredentialsKey], &revoked); err != nil {
		t.Fatal(err)
	}
	if len(revoked.Credentials) != 0 {
		t.Fatal("departed member retained gateway authority")
	}

}
