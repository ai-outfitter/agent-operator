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

	aioutfitterv1alpha1 "github.com/ai-outfitter/agent-operator/code/operator/api/v1alpha1"
)

const (
	AgentNameLabel  = "aioutfitter.com/agent"
	AgentUIDLabel   = "aioutfitter.com/agent-uid"
	ManagedByLabel  = "app.kubernetes.io/managed-by"
	RuntimeName     = "agent-runtime"
	WorkspaceName   = "agent-workspace"
	LimitRangeName  = "agent-workspace-defaults"
	SettingsName    = "outfitter-settings"
	WorkspaceMount  = "/workspace"
	CredentialsRoot = "/var/run/link/credentials"
	NixStoreName    = "agent-nix-store"
	NixMount        = "/nix"
	HomeEnvName     = "HOME"
)

var defaultWorkspaceSize = resource.MustParse("10Gi")

// The Nix store holds the image's runtime closure plus anything the agent
// installs at runtime, so it is sized generously.
var defaultNixStoreSize = resource.MustParse("20Gi")

// agentFSGroup makes the persistent volumes group-writable by the uid-1000
// agent process (and the seed init container), so single-user Nix can write the
// /nix store.
var agentFSGroup = ptr.To[int64](1000)

// Each image boot merges its immutable store closure into the persistent store.
// --no-clobber keeps the agent's existing store paths and Nix state intact.
// SOURCE_NIX and DESTINATION_NIX make the exact upgrade behavior testable without
// requiring a mounted image or PVC.
const nixStoreSeedScript = `set -eu
source_nix="${SOURCE_NIX:-/nix}"
destination_nix="${DESTINATION_NIX:-/mnt/nix}"
mkdir -p "$destination_nix/store"
cp -a --no-clobber "$source_nix/store/." "$destination_nix/store/"
touch "$destination_nix/.seeded"`

func agentNamespace(agentName string) string { return "agent-" + agentName }

func ownershipLabels(agent *aioutfitterv1alpha1.Agent) map[string]string {
	return map[string]string{
		AgentNameLabel: AgentNameLabelValue(agent.Name),
		AgentUIDLabel:  string(agent.UID),
		ManagedByLabel: "agent-operator",
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

func (r *AgentReconciler) ensureAgentNamespace(ctx context.Context, agent *aioutfitterv1alpha1.Agent) error {
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
	agent *aioutfitterv1alpha1.Agent,
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

	storageClass := agent.Spec.Workspace.Volume.StorageClassName
	workspaceSize := agent.Spec.Workspace.Volume.Size.DeepCopy()
	if workspaceSize.IsZero() {
		workspaceSize = defaultWorkspaceSize.DeepCopy()
	}
	if err := r.ensurePVC(ctx, namespace, WorkspaceName, labels, storageClass, workspaceSize); err != nil {
		return nil, err
	}
	// Persistent Nix store: seeded from the image on first boot, then writable so
	// agents can `nix profile install` tools that survive restarts.
	if err := r.ensurePVC(ctx, namespace, NixStoreName, labels, storageClass, defaultNixStoreSize.DeepCopy()); err != nil {
		return nil, err
	}

	return quota, nil
}

// ensurePVC creates or updates a ReadWriteOnce PVC. It preserves a StorageClass
// defaulted by admission — setting it back to nil on a bound claim would attempt
// to mutate an immutable PVC field.
func (r *AgentReconciler) ensurePVC(
	ctx context.Context,
	namespace, name string,
	labels map[string]string,
	storageClass *string,
	size resource.Quantity,
) error {
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, claim, func() error {
		claim.Labels = mergeLabels(claim.Labels, labels)
		claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		if storageClass != nil || claim.CreationTimestamp.IsZero() {
			claim.Spec.StorageClassName = storageClass
		}
		claim.Spec.Resources.Requests = corev1.ResourceList{corev1.ResourceStorage: size}
		return nil
	})
	return err
}

// pvcVolume is a pod volume backed by the same-named PersistentVolumeClaim.
func pvcVolume(name string) corev1.Volume {
	return corev1.Volume{
		Name: name,
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: name,
		}},
	}
}

func (r *AgentReconciler) ensureAgentDeployment(
	ctx context.Context,
	agent *aioutfitterv1alpha1.Agent,
	organization *aioutfitterv1alpha1.Organization,
) (*appsv1.Deployment, error) {
	namespace := agentNamespace(agent.Name)
	labels := ownershipLabels(agent)
	runtimeImage := r.agentImage(agent)
	selectorLabels := map[string]string{
		"app.kubernetes.io/name":     "agent-runtime",
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
			RunAsUser:    agentFSGroup,
			RunAsGroup:   agentFSGroup,
			FSGroup:      agentFSGroup,
		}

		container := corev1.Container{
			Name:            "agent",
			Image:           runtimeImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			WorkingDir:      WorkspaceMount,
			Env: []corev1.EnvVar{
				{Name: HomeEnvName, Value: WorkspaceMount},
				{Name: "AGENT_NAME", Value: agent.Name},
				{Name: "AGENT_SLUG", Value: agent.Spec.Profile.Agent},
				{Name: "AGENT_HARNESS", Value: agent.Spec.Profile.Harness},
				{Name: "AGENT_ORGANIZATION", Value: organization.Name},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: WorkspaceName, MountPath: WorkspaceMount},
				{Name: NixStoreName, MountPath: NixMount},
				{Name: SettingsName, MountPath: path.Join(WorkspaceMount, ".agents"), ReadOnly: true},
			},
		}
		volumes := []corev1.Volume{
			pvcVolume(WorkspaceName),
			pvcVolume(NixStoreName),
			{
				Name: SettingsName,
				VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: SettingsName},
				}},
			},
		}
		// Merge the current image's store paths on every boot. A prior .seeded
		// marker is informational only: image upgrades can introduce new hashes,
		// while --no-clobber preserves paths and Nix state already on the PVC.
		seedInit := corev1.Container{
			Name:            "seed-nix-store",
			Image:           runtimeImage,
			ImagePullPolicy: corev1.PullIfNotPresent,
			Command:         []string{"sh", "-c", nixStoreSeedScript},
			VolumeMounts:    []corev1.VolumeMount{{Name: NixStoreName, MountPath: "/mnt/nix"}},
		}
		credentialEnvFrom, credentialMounts, credentialVolumes := credentialProjection(agent)
		container.EnvFrom = credentialEnvFrom
		container.VolumeMounts = append(container.VolumeMounts, credentialMounts...)
		volumes = append(volumes, credentialVolumes...)

		initContainers := make([]corev1.Container, 0, len(agent.Spec.Setup)+1)
		initContainers = append(initContainers, seedInit)
		for _, step := range agent.Spec.Setup {
			mounts := append([]corev1.VolumeMount{}, credentialMounts...)
			mounts = append(mounts,
				corev1.VolumeMount{Name: WorkspaceName, MountPath: WorkspaceMount},
				corev1.VolumeMount{Name: NixStoreName, MountPath: NixMount},
			)
			initContainers = append(initContainers, corev1.Container{
				Name:            "setup-" + step.Name,
				Image:           runtimeImage,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"sh", "-c", step.Script},
				EnvFrom:         credentialEnvFrom,
				Env:             []corev1.EnvVar{{Name: HomeEnvName, Value: WorkspaceMount}},
				VolumeMounts:    mounts,
			})
		}
		deployment.Spec.Template.Spec.InitContainers = initContainers
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
	agent *aioutfitterv1alpha1.Agent,
	organization *aioutfitterv1alpha1.Organization,
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

func credentialProjection(
	agent *aioutfitterv1alpha1.Agent,
) (envFrom []corev1.EnvFromSource, mounts []corev1.VolumeMount, volumes []corev1.Volume) {
	for _, reference := range agent.Spec.Credentials {
		name, kind := credentialObject(reference)
		if reference.As == aioutfitterv1alpha1.CredentialExposureEnv {
			envSource := corev1.EnvFromSource{}
			if kind == credentialKindSecret {
				envSource.SecretRef = &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}
			} else {
				envSource.ConfigMapRef = &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}
			}
			envFrom = append(envFrom, envSource)
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
		volumes = append(volumes, volume)
		mounts = append(mounts, corev1.VolumeMount{
			Name:      volumeName,
			MountPath: mountPath,
			ReadOnly:  true,
		})
	}
	return envFrom, mounts, volumes
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

func (r *AgentReconciler) agentImage(agent *aioutfitterv1alpha1.Agent) string {
	if agent.Spec.Image != "" {
		return agent.Spec.Image
	}
	if r.AgentImage != "" {
		return r.AgentImage
	}
	return "agent-runtime:dev"
}
