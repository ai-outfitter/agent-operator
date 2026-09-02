package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const AgentIsolationNetworkPolicyName = "agent-isolation"

func effectiveNetworkPolicyMode(agent *aioutfitterv1alpha1.Agent, organization *aioutfitterv1alpha1.Organization) aioutfitterv1alpha1.NetworkPolicyMode {
	if agent.Spec.NetworkPolicy != nil {
		return agent.Spec.NetworkPolicy.Mode
	}
	if organization.Spec.NetworkPolicy != nil {
		return organization.Spec.NetworkPolicy.Mode
	}
	return aioutfitterv1alpha1.NetworkPolicyModeUnmanaged
}

func (r *AgentReconciler) ensureAgentNetworkPolicy(ctx context.Context, agent *aioutfitterv1alpha1.Agent, organization *aioutfitterv1alpha1.Organization) error {
	key := types.NamespacedName{Namespace: agentNamespace(agent.Name), Name: AgentIsolationNetworkPolicyName}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	if effectiveNetworkPolicyMode(agent, organization) == aioutfitterv1alpha1.NetworkPolicyModeUnmanaged {
		if err := r.Get(ctx, key, policy); err != nil {
			return client.IgnoreNotFound(err)
		}
		if policy.Labels[AgentUIDLabel] != string(agent.UID) {
			return nil
		}
		return client.IgnoreNotFound(r.Delete(ctx, policy))
	}
	if err := r.Get(ctx, key, policy); err == nil {
		if policy.Labels[AgentUIDLabel] != string(agent.UID) {
			return fmt.Errorf("NetworkPolicy %s/%s already exists and is not owned by Agent %q", key.Namespace, key.Name, agent.Name)
		}
	} else {
		if client.IgnoreNotFound(err) != nil {
			return err
		}
		policy = &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	}

	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	dnsPort := intstr.FromInt32(53)
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		policy.Labels = mergeLabels(policy.Labels, ownershipLabels(agent))
		policy.Spec = networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{
				appNameLabel: RuntimeName, appInstanceLabel: agent.Name,
			}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
			Egress: []networkingv1.NetworkPolicyEgressRule{{Ports: []networkingv1.NetworkPolicyPort{
				{Protocol: &udp, Port: ptr.To(dnsPort)},
				{Protocol: &tcp, Port: ptr.To(dnsPort)},
			}}},
		}
		return nil
	})
	return err
}
