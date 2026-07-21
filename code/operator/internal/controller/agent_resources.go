package controller

import (
	"context"
	"fmt"
	"maps"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	linkv1alpha1 "github.com/ncrmro/link-operator/code/operator/api/v1alpha1"
)

const (
	AgentNameLabel  = "link.aioutfitter.com/agent"
	AgentUIDLabel   = "link.aioutfitter.com/agent-uid"
	ManagedByLabel  = "app.kubernetes.io/managed-by"
	RuntimeName     = "agent-runtime"
	WorkspaceName   = "agent-workspace"
	LimitRangeName  = "agent-workspace-defaults"
	SettingsName    = "outfitter-settings"
	WorkspaceMount  = "/workspace"
	CredentialsRoot = "/var/run/link/credentials"
)

var defaultWorkspaceSize = resource.MustParse("10Gi")

func agentNamespace(agentName string) string { return "agent-" + agentName }

func ownershipLabels(agent *linkv1alpha1.Agent) map[string]string {
	return map[string]string{
		AgentNameLabel: AgentNameLabelValue(agent.Name),
		AgentUIDLabel:  string(agent.UID),
		ManagedByLabel: "link-operator",
	}
}

// AgentNameLabelValue is kept separate to make the ownership label contract
// explicit and testable.
func AgentNameLabelValue(agentName string) string { return agentName }

func mergeLabels(existing, required map[string]string) map[string]string {
	if existing == nil {
		existing = make(map[string]string, len(required))
	}
	maps.Copy(existing, required)
	return existing
}

func (r *AgentReconciler) ensureAgentNamespace(ctx context.Context, agent *linkv1alpha1.Agent) error {
	namespace := &corev1.Namespace{}
	key := types.NamespacedName{Name: agentNamespace(agent.Name)}
	if err := r.Get(ctx, key, namespace); client.IgnoreNotFound(err) != nil {
		return err
	}
	if namespace.Name != "" && namespace.Labels[AgentUIDLabel] != "" && namespace.Labels[AgentUIDLabel] != string(agent.UID) {
		return fmt.Errorf("namespace %q is owned by a different Agent UID", key.Name)
	}
	if namespace.Name == "" {
		namespace.Name = key.Name
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, namespace, func() error {
		namespace.Labels = mergeLabels(namespace.Labels, ownershipLabels(agent))
		return nil
	})
	return err
}

func (r *AgentReconciler) ensureWorkspaceResources(
	ctx context.Context,
	agent *linkv1alpha1.Agent,
) (*corev1.ResourceQuota, error) {
	namespace := agentNamespace(agent.Name)
	labels := ownershipLabels(agent)

	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, serviceAccount, func() error {
		serviceAccount.Labels = mergeLabels(serviceAccount.Labels, labels)
		return nil
	}); err != nil {
		return nil, err
	}

	roleBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName + "-admin", Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, roleBinding, func() error {
		roleBinding.Labels = mergeLabels(roleBinding.Labels, labels)
		roleBinding.RoleRef = rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "admin"}
		roleBinding.Subjects = []rbacv1.Subject{{Kind: "ServiceAccount", Name: RuntimeName, Namespace: namespace}}
		return nil
	}); err != nil {
		return nil, err
	}

	quota := &corev1.ResourceQuota{ObjectMeta: metav1.ObjectMeta{Name: WorkspaceName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, quota, func() error {
		quota.Labels = mergeLabels(quota.Labels, labels)
		quota.Spec.Hard = agent.Spec.Workspace.ResourceQuota.Hard.DeepCopy()
		return nil
	}); err != nil {
		return nil, err
	}

	limitRange := &corev1.LimitRange{ObjectMeta: metav1.ObjectMeta{Name: LimitRangeName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, limitRange, func() error {
		limitRange.Labels = mergeLabels(limitRange.Labels, labels)
		limitRange.Spec.Limits = []corev1.LimitRangeItem{{
			Type:           corev1.LimitTypeContainer,
			Default:        agent.Spec.Workspace.LimitRange.Container.Default.DeepCopy(),
			DefaultRequest: agent.Spec.Workspace.LimitRange.Container.DefaultRequest.DeepCopy(),
		}}
		return nil
	}); err != nil {
		return nil, err
	}

	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: WorkspaceName, Namespace: namespace}}
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, claim, func() error {
		claim.Labels = mergeLabels(claim.Labels, labels)
		claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		// Preserve a StorageClass defaulted by admission. Setting it back to nil
		// on a bound claim would attempt to mutate an immutable PVC field.
		if agent.Spec.Workspace.Volume.StorageClassName != nil || claim.CreationTimestamp.IsZero() {
			claim.Spec.StorageClassName = agent.Spec.Workspace.Volume.StorageClassName
		}
		size := agent.Spec.Workspace.Volume.Size.DeepCopy()
		if size.IsZero() {
			size = defaultWorkspaceSize.DeepCopy()
		}
		claim.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: size}
		return nil
	}); err != nil {
		return nil, err
	}

	return quota, nil
}

func (r *AgentReconciler) ensureAgentDeployment(
	ctx context.Context,
	agent *linkv1alpha1.Agent,
	organization *linkv1alpha1.Organization,
) (*appsv1.Deployment, error) {
	namespace := agentNamespace(agent.Name)
	labels := ownershipLabels(agent)
	selectorLabels := map[string]string{
		"app.kubernetes.io/name":     "link-agent",
		"app.kubernetes.io/instance": agent.Name,
	}
	maps.Copy(selectorLabels, labels)

	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: RuntimeName, Namespace: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		deployment.Labels = mergeLabels(deployment.Labels, labels)
		deployment.Spec.Replicas = ptr.To[int32](1)
		deployment.Spec.Strategy.Type = appsv1.RecreateDeploymentStrategyType
		deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels}
		deployment.Spec.Template.Labels = mergeLabels(selectorLabels, labels)
		deployment.Spec.Template.Spec.ServiceAccountName = RuntimeName
		deployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(true)
		deployment.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{
			RunAsNonRoot: ptr.To(true),
		}

		container := corev1.Container{
			Name:            "agent",
			Image:           r.agentImage(),
			ImagePullPolicy: corev1.PullIfNotPresent,
			WorkingDir:      WorkspaceMount,
			Env: []corev1.EnvVar{
				{Name: "HOME", Value: WorkspaceMount},
				{Name: "LINK_AGENT", Value: agent.Name},
				{Name: "LINK_AGENT_SLUG", Value: agent.Spec.Profile.Agent},
				{Name: "LINK_AGENT_HARNESS", Value: agent.Spec.Profile.Harness},
				{Name: "LINK_ORGANIZATION", Value: organization.Name},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{
					Command: []string{"test", "-s", path.Join(WorkspaceMount, ".link", "mail-loop-ready")},
				}},
				InitialDelaySeconds: 1,
				PeriodSeconds:       2,
				FailureThreshold:    15,
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: WorkspaceName, MountPath: WorkspaceMount},
				{Name: SettingsName, MountPath: path.Join(WorkspaceMount, ".agents"), ReadOnly: true},
			},
		}
		volumes := []corev1.Volume{{
			Name: WorkspaceName,
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: WorkspaceName,
			}},
		}, {
			Name: SettingsName,
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: SettingsName},
			}},
		}}
		appendCredentialProjection(agent, &container, &volumes)
		deployment.Spec.Template.Spec.Containers = []corev1.Container{container}
		deployment.Spec.Template.Spec.Volumes = volumes
		return nil
	})
	return deployment, err
}

type outfitterSettings struct {
	DefaultAgent   string            `json:"default_agent"`
	DefaultHarness string            `json:"default_harness"`
	CacheDirectory string            `json:"cache_directory"`
	Sources        []outfitterSource `json:"sources"`
}

type outfitterSource struct {
	GitHub *string `json:"github,omitempty"`
	URI    *string `json:"uri,omitempty"`
	Path   string  `json:"path,omitempty"`
	Ref    string  `json:"ref,omitempty"`
}

func (r *AgentReconciler) ensureOutfitterSettings(
	ctx context.Context,
	agent *linkv1alpha1.Agent,
	organization *linkv1alpha1.Organization,
) error {
	catalogSource := organization.Spec.AgentCatalogs[0]
	source := outfitterSource{GitHub: catalogSource.GitHub, URI: catalogSource.URI}
	if catalogSource.LocalPath != nil {
		source.Path = *catalogSource.LocalPath
	} else {
		source.Path = catalogSource.Path
	}
	if catalogSource.Revision != nil {
		source.Ref = *catalogSource.Revision
	}
	settings, err := yaml.Marshal(outfitterSettings{
		DefaultAgent:   agent.Spec.Profile.Agent,
		DefaultHarness: agent.Spec.Profile.Harness,
		CacheDirectory: path.Join(WorkspaceMount, ".agents-cache"),
		Sources:        []outfitterSource{source},
	})
	if err != nil {
		return err
	}
	configMap := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: SettingsName, Namespace: agentNamespace(agent.Name),
	}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, configMap, func() error {
		configMap.Labels = mergeLabels(configMap.Labels, ownershipLabels(agent))
		configMap.Data = map[string]string{"settings.yml": string(settings)}
		return nil
	})
	return err
}

func appendCredentialProjection(
	agent *linkv1alpha1.Agent,
	container *corev1.Container,
	volumes *[]corev1.Volume,
) {
	for _, reference := range agent.Spec.Credentials {
		name, kind := credentialObject(reference)
		if reference.As == linkv1alpha1.CredentialExposureEnv {
			envSource := corev1.EnvFromSource{}
			if kind == credentialKindSecret {
				envSource.SecretRef = &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}
			} else {
				envSource.ConfigMapRef = &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}
			}
			container.EnvFrom = append(container.EnvFrom, envSource)
			continue
		}

		volumeName := credentialVolumeName(kind, name)
		volume := corev1.Volume{Name: volumeName}
		mountPath := path.Join(CredentialsRoot, strings.ToLower(kind)+"s", name)
		if kind == credentialKindSecret {
			volume.Secret = &corev1.SecretVolumeSource{SecretName: name}
		} else {
			volume.ConfigMap = &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}
		}
		*volumes = append(*volumes, volume)
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}
}

func credentialVolumeName(kind, name string) string {
	prefix := "config-"
	if kind == credentialKindSecret {
		prefix = "secret-"
	}
	value := prefix + name
	if len(value) > 63 {
		value = value[:63]
		value = strings.TrimRight(value, "-")
	}
	return value
}

func (r *AgentReconciler) agentImage() string {
	if r.AgentImage != "" {
		return r.AgentImage
	}
	return "link-agent:dev"
}
