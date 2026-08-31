package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const (
	ForgeGatewayName         = "forge-gateway"
	ForgeGatewaySecretName   = "forge-gateway"
	ForgeRoutesConfigMapName = "forge-routes"
	ForgeSpoolPVCName        = "forge-spool"
	OrganizationLabel        = "aioutfitter.com/organization"
	appNameLabel             = "app.kubernetes.io/name"
	appInstanceLabel         = "app.kubernetes.io/instance"
	namespaceNameLabel       = "kubernetes.io/metadata.name"
	forgeRoutesKey           = "routes.json"
	httpPortName             = "http"
)

type gatewayRoute struct{ Username, URL, Token string }
type publicRoute struct{ Username, URL string }

func organizationNamespace(name string) string { return "org-" + name }

//nolint:unparam // The controller result is retained for reconciler composition and future requeues.
func (r *OrganizationReconciler) reconcileForgeGateway(ctx context.Context, org *aioutfitterv1alpha1.Organization) (ctrl.Result, error) {
	if org.Spec.Forge == nil {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeGatewayReady, metav1.ConditionFalse, "NotConfigured", "Forge gateway is not configured")
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeRoutesReady, metav1.ConditionFalse, "NotConfigured", "Forge routes are not configured")
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionFalse, "NotConfigured", "Webhook endpoint is not configured")
		return ctrl.Result{}, nil
	}
	forge := org.Spec.Forge
	agents := &aioutfitterv1alpha1.AgentList{}
	if err := r.List(ctx, agents); err != nil {
		return ctrl.Result{}, err
	}
	var members []aioutfitterv1alpha1.Agent
	seen := map[string]string{}
	for i := range agents.Items {
		agent := &agents.Items[i]
		member := false
		for _, m := range agent.Spec.Memberships {
			if m.Organization == org.Name {
				member = true
				break
			}
		}
		if !member || agent.Spec.Forge == nil {
			continue
		}
		key := strings.ToLower(agent.Spec.Forge.Username)
		if prior, duplicate := seen[key]; duplicate {
			message := fmt.Sprintf("Agents %q and %q declare duplicate case-insensitive forge username %q", prior, agent.Name, key)
			setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeRoutesReady, metav1.ConditionFalse, "DuplicateUsername", message)
			setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeGatewayReady, metav1.ConditionFalse, "RoutesUnready", "Gateway is blocked by ambiguous routes")
			setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionFalse, "RoutesUnready", "Webhook endpoint is blocked by ambiguous routes")
			return ctrl.Result{}, nil
		}
		seen[key] = agent.Name
		members = append(members, *agent)
	}
	if len(members) == 0 {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeRoutesReady, metav1.ConditionFalse, "NoRoutes", "No member Agent declares spec.forge.username")
		return ctrl.Result{}, nil
	}
	slices.SortFunc(members, func(a, b aioutfitterv1alpha1.Agent) int {
		return strings.Compare(strings.ToLower(a.Spec.Forge.Username), strings.ToLower(b.Spec.Forge.Username))
	})
	ns := organizationNamespace(org.Name)
	labels := map[string]string{OrganizationLabel: org.Name, "app.kubernetes.io/managed-by": "agent-operator"}
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, namespace, func() error {
		namespace.Labels = mergeLabels(namespace.Labels, labels)
		return controllerutil.SetControllerReference(org, namespace, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}
	webhook := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: forge.Webhook.SecretName}, webhook); err != nil {
		reason := "WebhookSecretMissing"
		if !apierrors.IsNotFound(err) {
			return ctrl.Result{}, err
		}
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionFalse, reason, "Webhook HMAC Secret does not exist")
		return ctrl.Result{}, nil
	}
	webhookKey := webhook.Data["secret"]
	if len(webhookKey) == 0 {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionFalse, "WebhookSecretInvalid", "Webhook HMAC Secret must contain key secret")
		return ctrl.Result{}, nil
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: ForgeGatewaySecretName, Namespace: ns}}
	_ = r.Get(ctx, client.ObjectKeyFromObject(secret), secret)
	var prior []gatewayRoute
	_ = json.Unmarshal(secret.Data[forgeRoutesKey], &prior)
	priorTokens := map[string]string{}
	for _, x := range prior {
		priorTokens[strings.ToLower(x.Username)] = x.Token
	}
	var routes []gatewayRoute
	var public []publicRoute
	for i := range members {
		agent := &members[i]
		username := strings.ToLower(agent.Spec.Forge.Username)
		token := priorTokens[username]
		if token == "" {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				return ctrl.Result{}, err
			}
			token = base64.RawURLEncoding.EncodeToString(b)
		}
		url := fmt.Sprintf("http://%s.%s.svc:%d", RuntimeName, agentNamespace(agent.Name), A2APort)
		routes = append(routes, gatewayRoute{Username: username, URL: url, Token: token})
		public = append(public, publicRoute{Username: username, URL: url})
		if err := r.ensureAgentA2A(ctx, org, agent, token, labels); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.cleanupStaleAgentA2A(ctx, org, members); err != nil {
		return ctrl.Result{}, err
	}
	routesJSON, _ := json.Marshal(routes)
	publicJSON, _ := json.Marshal(public)
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Labels = mergeLabels(secret.Labels, labels)
		secret.Data = map[string][]byte{forgeRoutesKey: routesJSON, "webhook-secret": webhookKey}
		return controllerutil.SetControllerReference(org, secret, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: ForgeRoutesConfigMapName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
		cm.Labels = mergeLabels(cm.Labels, labels)
		cm.Data = map[string]string{forgeRoutesKey: string(publicJSON)}
		return controllerutil.SetControllerReference(org, cm, r.Scheme)
	}); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.ensureGatewayResources(ctx, org, labels); err != nil {
		return ctrl.Result{}, err
	}
	setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeRoutesReady, metav1.ConditionTrue, "Ready", fmt.Sprintf("%d forge routes are unambiguous", len(routes)))
	deployment := &appsv1.Deployment{}
	_ = r.Get(ctx, types.NamespacedName{Namespace: ns, Name: ForgeGatewayName}, deployment)
	if deployment.Status.AvailableReplicas > 0 {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeGatewayReady, metav1.ConditionTrue, "Ready", "Forge gateway Deployment is available")
	} else {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionForgeGatewayReady, metav1.ConditionFalse, "DeploymentUnavailable", "Forge gateway Deployment has no available replica")
	}
	ingress := &networkingv1.Ingress{}
	_ = r.Get(ctx, types.NamespacedName{Namespace: ns, Name: ForgeGatewayName}, ingress)
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionTrue, "Ready", "Webhook Ingress is admitted")
	} else {
		setOrganizationCondition(org, aioutfitterv1alpha1.OrganizationConditionWebhookEndpointReady, metav1.ConditionFalse, "IngressNotAdmitted", "Webhook Ingress is not admitted")
	}
	return ctrl.Result{}, nil
}

func (r *OrganizationReconciler) cleanupStaleAgentA2A(ctx context.Context, org *aioutfitterv1alpha1.Organization, members []aioutfitterv1alpha1.Agent) error {
	desired := make(map[string]struct{}, len(members))
	for i := range members {
		desired[agentNamespace(members[i].Name)] = struct{}{}
	}
	selector := client.MatchingLabels{OrganizationLabel: org.Name}
	var secrets corev1.SecretList
	if err := r.List(ctx, &secrets, selector); err != nil {
		return err
	}
	for i := range secrets.Items {
		item := &secrets.Items[i]
		if item.Name == A2ACredentialsSecretName {
			if _, ok := desired[item.Namespace]; !ok {
				if err := r.Delete(ctx, item); client.IgnoreNotFound(err) != nil {
					return err
				}
			}
		}
	}
	var services corev1.ServiceList
	if err := r.List(ctx, &services, selector); err != nil {
		return err
	}
	for i := range services.Items {
		item := &services.Items[i]
		if item.Name == RuntimeName {
			if _, ok := desired[item.Namespace]; !ok {
				if err := r.Delete(ctx, item); client.IgnoreNotFound(err) != nil {
					return err
				}
			}
		}
	}
	var policies networkingv1.NetworkPolicyList
	if err := r.List(ctx, &policies, selector); err != nil {
		return err
	}
	for i := range policies.Items {
		item := &policies.Items[i]
		if item.Name == "allow-forge-gateway-a2a" {
			if _, ok := desired[item.Namespace]; !ok {
				if err := r.Delete(ctx, item); client.IgnoreNotFound(err) != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *OrganizationReconciler) ensureAgentA2A(ctx context.Context, org *aioutfitterv1alpha1.Organization, agent *aioutfitterv1alpha1.Agent, token string, labels map[string]string) error {
	ns := agentNamespace(agent.Name)
	var namespace corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: ns}, &namespace); err != nil {
		return fmt.Errorf("agent namespace %q is not ready: %w", ns, err)
	}
	credentials, _ := json.Marshal(map[string]any{"credentials": []map[string]string{{"token": token, "principal": "forge-gateway:" + org.Name}}})
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: A2ACredentialsSecretName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Labels = mergeLabels(secret.Labels, labels)
		secret.Data = map[string][]byte{"credentials.json": credentials}
		return controllerutil.SetControllerReference(org, secret, r.Scheme)
	}); err != nil {
		return err
	}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Labels = mergeLabels(service.Labels, labels)
		service.Spec.Selector = map[string]string{appNameLabel: RuntimeName, appInstanceLabel: agent.Name}
		service.Spec.Ports = []corev1.ServicePort{{Name: "a2a", Port: A2APort, TargetPort: intstr.FromInt32(A2APort)}}
		return controllerutil.SetControllerReference(org, service, r.Scheme)
	}); err != nil {
		return err
	}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-forge-gateway-a2a", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		tcp := corev1.ProtocolTCP
		policy.Labels = mergeLabels(policy.Labels, labels)
		policy.Spec = networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{appNameLabel: RuntimeName, appInstanceLabel: agent.Name}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{OrganizationLabel: org.Name}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{appNameLabel: ForgeGatewayName}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptr.To(intstr.FromInt32(A2APort))}}}}}
		return controllerutil.SetControllerReference(org, policy, r.Scheme)
	})
	return err
}

func (r *OrganizationReconciler) ensureGatewayResources(ctx context.Context, org *aioutfitterv1alpha1.Organization, labels map[string]string) error {
	forge := org.Spec.Forge
	ns := organizationNamespace(org.Name)
	selector := map[string]string{appNameLabel: ForgeGatewayName, appInstanceLabel: org.Name}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: ForgeSpoolPVCName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
		pvc.Labels = mergeLabels(pvc.Labels, labels)
		pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		pvc.Spec.StorageClassName = forge.Spool.StorageClassName
		size := forge.Spool.Size
		if size.IsZero() {
			size = resource.MustParse("1Gi")
		}
		pvc.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: size}
		return controllerutil.SetControllerReference(org, pvc, r.Scheme)
	}); err != nil {
		return err
	}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: ForgeGatewayName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Labels = mergeLabels(deployment.Labels, labels)
		deployment.Spec.Replicas = ptr.To[int32](1)
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}
		deployment.Spec.Template.Labels = mergeLabels(deployment.Spec.Template.Labels, selector)
		deployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
		deployment.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true), RunAsUser: ptr.To[int64](1000), RunAsGroup: ptr.To[int64](1000), FSGroup: ptr.To[int64](1000), SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}
		deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: ForgeGatewayName, Image: r.GatewayImage, Command: []string{"/bin/manager"}, Args: []string{"forge-gateway"}, Ports: []corev1.ContainerPort{{Name: httpPortName, ContainerPort: 8080}}, Env: []corev1.EnvVar{{Name: "ORGANIZATION", Value: org.Name}, {Name: "FORGE_OWNER", Value: forge.Owner}, {Name: "SPOOL_PATH", Value: "/var/lib/forge-gateway/spool.db"}, {Name: "FORGE_ROUTES", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ForgeGatewaySecretName}, Key: forgeRoutesKey}}}, {Name: "FORGE_WEBHOOK_SECRET", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ForgeGatewaySecretName}, Key: "webhook-secret"}}}}, VolumeMounts: []corev1.VolumeMount{{Name: "spool", MountPath: "/var/lib/forge-gateway"}}, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false), ReadOnlyRootFilesystem: ptr.To(true), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("50m"), corev1.ResourceMemory: resource.MustParse("64Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m"), corev1.ResourceMemory: resource.MustParse("256Mi")}}}}
		deployment.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "spool", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: ForgeSpoolPVCName}}}}
		return controllerutil.SetControllerReference(org, deployment, r.Scheme)
	}); err != nil {
		return err
	}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: ForgeGatewayName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		service.Labels = mergeLabels(service.Labels, labels)
		service.Spec.Selector = selector
		service.Spec.Ports = []corev1.ServicePort{{Name: httpPortName, Port: 80, TargetPort: intstr.FromInt32(8080)}}
		return controllerutil.SetControllerReference(org, service, r.Scheme)
	}); err != nil {
		return err
	}
	pathType := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: ForgeGatewayName, Namespace: ns}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		ingress.Labels = mergeLabels(ingress.Labels, labels)
		ingress.Spec.IngressClassName = forge.Webhook.IngressClassName
		ingress.Spec.Rules = []networkingv1.IngressRule{{Host: forge.Webhook.Host, IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{Path: "/webhooks/forgejo", PathType: &pathType, Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: ForgeGatewayName, Port: networkingv1.ServiceBackendPort{Number: 80}}}}}}}}}
		if forge.Webhook.TLSSecretName != nil {
			ingress.Spec.TLS = []networkingv1.IngressTLS{{Hosts: []string{forge.Webhook.Host}, SecretName: *forge.Webhook.TLSSecretName}}
		}
		return controllerutil.SetControllerReference(org, ingress, r.Scheme)
	}); err != nil {
		return err
	}
	return r.ensureGatewayPolicies(ctx, org, labels, selector)
}

func (r *OrganizationReconciler) ensureGatewayPolicies(ctx context.Context, org *aioutfitterv1alpha1.Organization, labels, selector map[string]string) error {
	ns := organizationNamespace(org.Name)
	tcp := corev1.ProtocolTCP
	udp := corev1.ProtocolUDP
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "forge-gateway-isolation", Namespace: ns}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		policy.Labels = mergeLabels(policy.Labels, labels)
		peers := []networkingv1.NetworkPolicyPeer{}
		for _, m := range orgAgents(ctx, r.Client, org.Name) {
			peers = append(peers, networkingv1.NetworkPolicyPeer{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: agentNamespace(m)}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{appNameLabel: RuntimeName}}})
		}
		policy.Spec = networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: selector}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}, Ingress: []networkingv1.NetworkPolicyIngressRule{{From: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: "ingress-nginx"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptr.To(intstr.FromInt32(8080))}}}}, Egress: []networkingv1.NetworkPolicyEgressRule{{To: peers, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: ptr.To(intstr.FromInt32(A2APort))}}}, {To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{namespaceNameLabel: "kube-system"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: &udp, Port: ptr.To(intstr.FromInt32(53))}, {Protocol: &tcp, Port: ptr.To(intstr.FromInt32(53))}}}}}
		return controllerutil.SetControllerReference(org, policy, r.Scheme)
	})
	return err
}
func orgAgents(ctx context.Context, c client.Client, org string) []string {
	var list aioutfitterv1alpha1.AgentList
	_ = c.List(ctx, &list)
	var out []string
	for i := range list.Items {
		for _, m := range list.Items[i].Spec.Memberships {
			if m.Organization == org && list.Items[i].Spec.Forge != nil {
				out = append(out, list.Items[i].Name)
			}
		}
	}
	return out
}
