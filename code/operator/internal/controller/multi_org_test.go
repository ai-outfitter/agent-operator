package controller

import (
	"context"
	"strings"
	"testing"

	a "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	multiPrimary   = "primary"
	multiSecondary = "secondary"
)

func TestMultiOrgMembershipValidation(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = a.AddToScheme(scheme)
	primary := &a.Organization{ObjectMeta: metav1.ObjectMeta{Name: multiPrimary}}
	secondary := &a.Organization{ObjectMeta: metav1.ObjectMeta{Name: multiSecondary}}
	for _, o := range []*a.Organization{primary, secondary} {
		o.Status.Conditions = []metav1.Condition{{Type: a.OrganizationConditionAccepted, Status: metav1.ConditionTrue, Reason: "Accepted", LastTransitionTime: metav1.Now()}}
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(primary, secondary).Build()
	r := &AgentReconciler{Client: c, Scheme: scheme}
	agent := validAgent("shared", primary.Name)
	agent.Spec.Profile = a.AgentProfile{}
	agent.Spec.Pi = &a.PiSpec{Model: "multi-org/model"}
	agent.Spec.Memberships = append(agent.Spec.Memberships, a.Membership{Organization: secondary.Name})
	org, msg, err := r.validateAgent(context.Background(), agent)
	if err != nil || msg != "" || org.Name != primary.Name {
		t.Fatalf("primary identity: %v %s %v", org, msg, err)
	}
	for _, tc := range []struct{ org, project, want string }{{multiPrimary, "", "Duplicate"}, {"missing", "", "does not exist"}, {multiSecondary, "absent", "absent"}} {
		agent.Spec.Memberships[1] = a.Membership{Organization: tc.org}
		if tc.project != "" {
			agent.Spec.Memberships[1].Projects = []string{tc.project}
		}
		_, msg, err = r.validateAgent(context.Background(), agent)
		if err != nil || !strings.Contains(msg, tc.want) {
			t.Fatalf("invalid membership accepted: %s %v", msg, err)
		}
	}
}
func TestMultiOrgGatewaySharedResources(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, networkingv1.AddToScheme, a.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	p := &a.Organization{ObjectMeta: metav1.ObjectMeta{Name: multiPrimary, UID: "primary-uid"}}
	s := &a.Organization{ObjectMeta: metav1.ObjectMeta{Name: multiSecondary, UID: "secondary-uid"}}
	agent := validAgent("shared", p.Name)
	agent.UID = "shared-uid"
	agent.Spec.Memberships = append(agent.Spec.Memberships, a.Membership{Organization: s.Name})
	ns := agentNamespace(agent.Name)
	owner := metav1.OwnerReference{APIVersion: a.GroupVersion.String(), Kind: organizationKind, Name: p.Name, UID: p.UID, Controller: ptr.To(true)}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName, Namespace: ns, OwnerReferences: []metav1.OwnerReference{owner}, Labels: map[string]string{OrganizationLabel: p.Name}}}
	legacy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: legacyGatewayPolicyName, Namespace: ns, OwnerReferences: []metav1.OwnerReference{owner}, Labels: map[string]string{OrganizationLabel: p.Name}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(p, s, agent, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}, svc, legacy).Build()
	r := &OrganizationReconciler{Client: c, Scheme: scheme}
	for _, o := range []*a.Organization{p, s, p, s} {
		if err := r.ensureAgentA2A(ctx, o, agent, "token-"+o.Name, map[string]string{OrganizationLabel: o.Name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(ctx, types.NamespacedName{Name: RuntimeName, Namespace: ns}, svc); err != nil {
		t.Fatal(err)
	}
	if owner := metav1.GetControllerOf(svc); owner == nil || owner.Kind != "Agent" || owner.UID != agent.UID {
		t.Fatalf("service owner: %v", owner)
	}
	if svc.Labels[OrganizationLabel] != p.Name {
		t.Fatal("service primary changed")
	}
	secret := &corev1.Secret{}
	key := types.NamespacedName{Name: agentCredentialSecretName(agent), Namespace: ns}
	if err := c.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	if secret.Labels[OrganizationLabel] != p.Name {
		t.Fatal("secret primary changed")
	}
	for _, o := range []*a.Organization{p, s} {
		if !strings.Contains(string(secret.Data[agentA2ACredentialsKey]), "forge-gateway:"+o.Name) {
			t.Fatal("principal lost")
		}
		policy := &networkingv1.NetworkPolicy{}
		if err := c.Get(ctx, types.NamespacedName{Name: "allow-forge-gateway-a2a-" + o.Name, Namespace: ns}, policy); err != nil {
			t.Fatal(err)
		}
		if policy.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels[namespaceNameLabel] != organizationNamespace(o.Name) {
			t.Fatal("wrong gateway scope")
		}
	}
	if err := c.Get(ctx, types.NamespacedName{Name: legacyGatewayPolicyName, Namespace: ns}, &networkingv1.NetworkPolicy{}); !apierrors.IsNotFound(err) {
		t.Fatalf("owned legacy policy was not removed after replacement: %v", err)
	}
	verifyMultiOrgCredentialConvergence(t, r, agent, p, s)
	verifySharedServiceRepair(t, r, agent, s, svc)
	verifyCleanupOptimisticLock(t, r, agent, s)
	if err := r.cleanupStaleAgentA2A(ctx, s, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: RuntimeName, Namespace: ns}, svc); err != nil {
		t.Fatal("shared service deleted", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: "allow-forge-gateway-a2a-primary", Namespace: ns}, &networkingv1.NetworkPolicy{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	raw := string(secret.Data[agentA2ACredentialsKey])
	if strings.Contains(raw, "forge-gateway:secondary") || !strings.Contains(raw, "forge-gateway:primary") {
		t.Fatal("wrong credential cleaned")
	}
	// Removing the primary gateway must not delete the Agent-owned Service,
	// even though its stable organization label matches that cleanup selector.
	if err := r.ensureAgentA2A(ctx, s, agent, "token-secondary", map[string]string{OrganizationLabel: s.Name}); err != nil {
		t.Fatal(err)
	}
	if err := r.cleanupStaleAgentA2A(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: RuntimeName, Namespace: ns}, svc); err != nil {
		t.Fatal("primary cleanup deleted shared Service", err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: "allow-forge-gateway-a2a-" + s.Name, Namespace: ns}, &networkingv1.NetworkPolicy{}); err != nil {
		t.Fatal("primary cleanup deleted secondary policy", err)
	}
	if err := c.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	if raw := string(secret.Data[agentA2ACredentialsKey]); strings.Contains(raw, "forge-gateway:primary") || !strings.Contains(raw, "forge-gateway:secondary") {
		t.Fatal("primary cleanup changed secondary principal")
	}

}

func verifySharedServiceRepair(t *testing.T, r *OrganizationReconciler, agent *a.Agent, s *a.Organization, svc *corev1.Service) {
	t.Helper()
	ctx := context.Background()
	c := r.Client
	ns := agentNamespace(agent.Name)
	requests := r.organizationsForAgentService(ctx, svc)
	if len(requests) != 2 || requests[0].Name != agent.Spec.Memberships[0].Organization || requests[1].Name != s.Name {
		t.Fatalf("Service watch did not enqueue both organizations: %v", requests)
	}
	// A delete event carries the old Agent owner, allowing its gateways to recreate it.
	deleted := svc.DeepCopy()
	if err := c.Delete(ctx, svc); err != nil {
		t.Fatal(err)
	}
	if len(r.organizationsForAgentService(ctx, deleted)) != 2 {
		t.Fatal("deleted Service event lost organization routing")
	}
	if err := r.ensureAgentA2A(ctx, s, agent, "token-secondary", map[string]string{OrganizationLabel: s.Name}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, types.NamespacedName{Name: RuntimeName, Namespace: ns}, svc); err != nil {
		t.Fatal("Service was not repaired", err)
	}
}

func verifyCleanupOptimisticLock(t *testing.T, r *OrganizationReconciler, agent *a.Agent, org *a.Organization) {
	t.Helper()
	original := r.Client
	var injected bool
	r.Client = interceptor.NewClient(original.(client.WithWatch), interceptor.Funcs{Patch: func(ctx context.Context, inner client.WithWatch, object client.Object, patch client.Patch, options ...client.PatchOption) error {
		if secret, ok := object.(*corev1.Secret); ok && !injected {
			injected = true
			latest := &corev1.Secret{}
			if err := inner.Get(ctx, client.ObjectKeyFromObject(secret), latest); err != nil {
				return err
			}
			raw, err := mergeGatewayCredential(latest.Data[agentA2ACredentialsKey], "voice-preview", "voice-kept")
			if err != nil {
				return err
			}
			latest.Data[agentA2ACredentialsKey] = raw
			if err := inner.Update(ctx, latest); err != nil {
				return err
			}
		}
		return inner.Patch(ctx, object, patch, options...)
	}})
	defer func() { r.Client = original }()
	if err := r.cleanupStaleAgentA2A(context.Background(), org, nil); !apierrors.IsConflict(err) {
		t.Fatalf("concurrent credential cleanup did not conflict: %v", err)
	}
	latest := &corev1.Secret{}
	if err := original.Get(context.Background(), types.NamespacedName{Name: agentCredentialSecretName(agent), Namespace: agentNamespace(agent.Name)}, latest); err != nil {
		t.Fatal(err)
	}
	raw := string(latest.Data[agentA2ACredentialsKey])
	if !strings.Contains(raw, "voice-preview") || !strings.Contains(raw, "forge-gateway:"+org.Name) {
		t.Fatal("stale cleanup overwrote concurrent principals")
	}
}

func verifyMultiOrgCredentialConvergence(t *testing.T, r *OrganizationReconciler, agent *a.Agent, orgs ...*a.Organization) {
	t.Helper()
	ctx := context.Background()
	key := types.NamespacedName{Name: agentCredentialSecretName(agent), Namespace: agentNamespace(agent.Name)}
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		t.Fatal(err)
	}
	stable := string(secret.Data[agentA2ACredentialsKey])
	for range 5 {
		for _, org := range orgs {
			if err := r.ensureAgentA2A(ctx, org, agent, "token-"+org.Name, map[string]string{OrganizationLabel: org.Name}); err != nil {
				t.Fatal(err)
			}
			if err := r.Get(ctx, key, secret); err != nil {
				t.Fatal(err)
			}
			if string(secret.Data[agentA2ACredentialsKey]) != stable {
				t.Fatal("actual multi-org reconcile changed unchanged credential bytes")
			}
		}
	}
}
