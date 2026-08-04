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
	CredentialsRoot = "/var/run/agent/credentials"
	NixStoreName    = "agent-nix-store"
	NixMount        = "/nix"
	HomeEnvName     = "HOME"
	// BakedCatalogPath is the agent image's built-in Outfitter payload. The
	// entrypoint launches from $HOME, so this directory only enters the layer
	// stack through the trailing settings source rendered below — never as
	// the implicit workspace layer that would shadow the Organization catalog.
	BakedCatalogPath = "/opt/agent/.agents"

	// RelayPrincipalPrefix is a wire identity, not a product name: it appears
	// in every issued relay credential and keys the relay store's acknowledged
	// cursors. Renaming it without re-minting credentials and migrating that
	// store makes clients present a checkpoint the server does not recognise,
	// which hard-loops them — observed in production on 2026-08-03. It is
	// therefore deliberately excluded from the link → agent rename and tracked
	// as its own migration.
	RelayPrincipalPrefix = "link:"

	// BrowserName is the sidecar container serving the Chrome DevTools
	// Protocol; BrowserCDPURL is where the agent container reaches it over
	// pod-shared localhost. The address is loopback on purpose: nothing
	// outside the Pod can speak CDP, which is an unauthenticated
	// full-control protocol.
	BrowserName          = "browser"
	BrowserCDPPort       = 9222
	BrowserCDPURL        = "http://127.0.0.1:9222"
	BrowserCDPURLEnvName = "AGENT_BROWSER_CDP_URL"

	// APITokenVolumeName carries a manually projected ServiceAccount token.
	// Pod-level automountServiceAccountToken is false so sidecars (notably the
	// unsandboxed browser) never see the agent-runtime credentials; the token
	// is mounted into the agent container only, at the well-known path client
	// libraries expect.
	APITokenVolumeName = "agent-api-access"
	APITokenMountPath  = "/var/run/secrets/kubernetes.io/serviceaccount"
)

// apiTokenExpirationSeconds matches the kubelet-managed kube-api-access
// projection: one hour plus a small skew so clients refresh before expiry.
var apiTokenExpirationSeconds = ptr.To[int64](3607)

// defaultBrowserImage runs headless Chromium with no services of its own; the
// operator supplies every flag. Pinned by digest so an operator upgrade, not a
// registry tag move, is what changes the browser. The tag is kept for
// readability; the digest is what the runtime resolves.
const defaultBrowserImage = "docker.io/chromedp/headless-shell:151.0.7922.72" +
	"@sha256:c65aef2b8fef5113cb97be8c99f7bf094320ca9b11e511041e6924e516bda0a1"

// defaultBrowserCommand bypasses the image entrypoint: headless-shell's wrapper
// prepends its own debugging flags and starts a socat forwarder that listens on
// 0.0.0.0:9222 — exactly the off-pod CDP exposure the sidecar must not have.
// Running the binary directly keeps the listener loopback.
var defaultBrowserCommand = []string{"/headless-shell/headless-shell"}

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

// apiTokenVolume reproduces the kubelet's kube-api-access projection — a bound
// ServiceAccount token plus the cluster CA and namespace — as an explicit
// volume, so the token can be mounted into the agent container only. The
// audience is left empty on purpose: it defaults to the API server audience,
// which is what kubectl in the agent container needs.
func apiTokenVolume() corev1.Volume {
	return corev1.Volume{
		Name: APITokenVolumeName,
		VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
					Path:              "token",
					ExpirationSeconds: apiTokenExpirationSeconds,
				}},
				{ConfigMap: &corev1.ConfigMapProjection{
					LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"},
					Items:                []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
				}},
				{DownwardAPI: &corev1.DownwardAPIProjection{
					Items: []corev1.DownwardAPIVolumeFile{{
						Path:     "namespace",
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
					}},
				}},
			},
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
		// No pod-wide token automount: the browser sidecar runs an
		// unsandboxed Chromium and must never hold agent-runtime API
		// credentials. The agent container gets an explicit projected token
		// instead (see apiTokenVolume).
		deployment.Spec.Template.Spec.AutomountServiceAccountToken = ptr.To(false)
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
			// The resident invocation, stated here rather than assumed from the image.
			//
			// The stock Outfitter container's entrypoint is bare `outfitter`, which prints
			// usage and exits, so without these the default image yields an agent that never
			// starts. Depending on a baked entrypoint is precisely what tied this operator to
			// an image it had to publish itself.
			//
			// Stdin keeps the RPC session alive: the harness stays available while its
			// extensions wait for work, with no polling and no initial model turn. A
			// user-supplied image carrying its own entrypoint ignores these arguments.
			Args: []string{
				"run",
				agent.Spec.Profile.Agent,
				"--strict",
				"--",
				"--mode",
				"rpc",
				"--no-session",
			},
			Stdin: true,
			Env: []corev1.EnvVar{
				{Name: HomeEnvName, Value: WorkspaceMount},
				{Name: "AGENT_NAME", Value: agent.Name},
				{Name: "AGENT_SLUG", Value: agent.Spec.Profile.Agent},
				{Name: "AGENT_HARNESS", Value: agent.Spec.Profile.Harness},
				{Name: "AGENT_ORGANIZATION", Value: organization.Name},
				{
					Name:  "AGENT_ENDPOINT_ID",
					Value: RelayPrincipalPrefix + agent.Name,
				},
				{
					Name:  "AGENT_PRINCIPAL_ID",
					Value: RelayPrincipalPrefix + agent.Name,
				},
				{
					Name:  "AGENT_SPOOL_PATH",
					Value: path.Join(WorkspaceMount, ".channels", "agent"),
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{Name: WorkspaceName, MountPath: WorkspaceMount},
				{Name: NixStoreName, MountPath: NixMount},
				{Name: SettingsName, MountPath: path.Join(WorkspaceMount, ".agents"), ReadOnly: true},
				{Name: APITokenVolumeName, MountPath: APITokenMountPath, ReadOnly: true},
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
			apiTokenVolume(),
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
				// Setup scripts are user-supplied bootstrap and could always
				// reach the API server; pod-wide automount is off, so the
				// projection has to be mounted explicitly to preserve that.
				// seed-nix-store is deliberately excluded: it only copies the
				// image's store closure onto the PVC.
				corev1.VolumeMount{Name: APITokenVolumeName, MountPath: APITokenMountPath, ReadOnly: true},
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
		containers := []corev1.Container{container}
		if browser := agent.Spec.Browser; browser != nil && browser.Enabled {
			container.Env = append(container.Env,
				corev1.EnvVar{Name: BrowserCDPURLEnvName, Value: BrowserCDPURL})
			containers = []corev1.Container{container, browserSidecar(browser)}
			volumes = append(volumes, corev1.Volume{
				Name:         browserDataName,
				VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
			})
		}
		deployment.Spec.Template.Spec.Containers = containers
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
		// The baked payload trails the catalog: the entrypoint launches from
		// $HOME so the image's /opt/agent/.agents is no longer the implicit
		// workspace layer (which outranks every source and shadowed the
		// catalog's root files). Rendering it as the LAST source keeps its
		// root system-prompt.md, skills, and the researcher fallback agent
		// resolvable while the catalog wins wherever both define a resource.
		Sources: []outfitterSource{source, {Path: BakedCatalogPath}},
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

const browserDataName = "browser-data"

// browserSidecar builds the headless-Chrome container. The Pod securityContext
// already forces uid/gid 1000, which is why --no-sandbox is required: the
// Chrome sandbox needs either root-owned helpers or user namespaces, and the
// agent Pod grants neither. The DevTools listener binds loopback only.
func browserSidecar(browser *aioutfitterv1alpha1.BrowserSpec) corev1.Container {
	image := browser.Image
	if image == "" {
		image = defaultBrowserImage
	}
	return corev1.Container{
		Name:            BrowserName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		// Not overridable by the Agent author: the upstream entrypoint
		// publishes CDP on 0.0.0.0, and CDP is unauthenticated full browser
		// control, so the listener address is the operator's decision alone.
		Command: defaultBrowserCommand,
		Args: []string{
			"--remote-debugging-address=127.0.0.1",
			fmt.Sprintf("--remote-debugging-port=%d", BrowserCDPPort),
			"--no-sandbox",
			"--disable-gpu",
			"--disable-dev-shm-usage",
			"--user-data-dir=/data",
		},
		Resources: browser.Resources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: browserDataName, MountPath: "/data"},
		},
	}
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
