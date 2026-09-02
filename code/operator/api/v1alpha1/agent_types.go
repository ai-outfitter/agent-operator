package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	AgentFinalizer = "agents.aioutfitter.com/finalizer"

	AgentConditionAccepted               = "Accepted"
	AgentConditionNamespaceReady         = "NamespaceReady"
	AgentConditionNetworkPolicyReady     = "NetworkPolicyReady"
	AgentConditionWorkspaceReady         = "WorkspaceReady"
	AgentConditionCredentialsReady       = "CredentialsReady"
	AgentConditionOutfitterSettingsReady = "OutfitterSettingsReady"
	AgentConditionWorkloadReady          = "WorkloadReady"
	AgentConditionReady                  = "Ready"
)

// NetworkPolicyMode controls whether the operator isolates an Agent Pod.
// +kubebuilder:validation:Enum=Unmanaged;Isolated
type NetworkPolicyMode string

const (
	NetworkPolicyModeUnmanaged NetworkPolicyMode = "Unmanaged"
	NetworkPolicyModeIsolated  NetworkPolicyMode = "Isolated"
)

// AgentNetworkPolicySpec configures the operator-owned network baseline.
type AgentNetworkPolicySpec struct {
	// Mode defaults to Unmanaged. Isolated denies all ingress and egress except
	// DNS and traffic admitted by additional NetworkPolicy resources.
	Mode NetworkPolicyMode `json:"mode"`
}

// Membership grants organization-level access and optionally names projects.
type Membership struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Organization string `json:"organization"`

	// An empty list means organization-level access, not every project.
	// +listType=set
	// +optional
	Projects []string `json:"projects,omitempty"`
}

// AgentProfile selects the slug and harness that Outfitter resolves at runtime.
type AgentProfile struct {
	// +kubebuilder:validation:MinLength=1
	Agent string `json:"agent"`

	// +kubebuilder:default=pi
	// +kubebuilder:validation:Enum=pi
	Harness string `json:"harness,omitempty"`
}

// GitHubSpec configures the resident runtime's GitHub notification source.
// Values supplied by credential env projections take precedence during
// migration. When NotifyOrgs is omitted, the operator derives the forge owner
// from the Organization's GitHub catalog shorthand when available.
type GitHubSpec struct {
	// +listType=set
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:Pattern=`^[A-Za-z0-9_.-]+$`
	NotifyOrgs []string `json:"notifyOrgs,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=1000
	// +kubebuilder:validation:Maximum=86400000
	PollMS *int64 `json:"pollMs,omitempty"`

	// +listType=set
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:Pattern=`^[a-z_]+$`
	Filters []string `json:"filters,omitempty"`
}

// BrowserSpec configures an optional headless-Chrome sidecar container. The
// sidecar serves the Chrome DevTools Protocol on 127.0.0.1:9222 inside the
// Pod, so a browser MCP server in the agent container attaches over pod-shared
// localhost with no Service. The agent container receives the endpoint as
// AGENT_BROWSER_CDP_URL.
type BrowserSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Image must place its Chromium binary at /headless-shell/headless-shell
	// and accept Chrome flags as arguments (the operator passes
	// --remote-debugging-address/-port, --no-sandbox, --disable-gpu,
	// --disable-dev-shm-usage, --user-data-dir). The operator always runs
	// that path directly rather than the image entrypoint, deliberately and
	// without an override: the upstream headless-shell entrypoint starts a
	// forwarder that publishes the DevTools listener on 0.0.0.0, and CDP is
	// unauthenticated full browser control, so an entrypoint must never
	// choose the listener address. Images with another layout are not
	// supported. When omitted, the operator's digest-pinned default browser
	// image is used.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image,omitempty"`

	// Resources for the sidecar container. When omitted, the workspace
	// LimitRange defaults apply. The sidecar counts against the agent
	// namespace's ResourceQuota.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SetupStep defines one initialization script that runs before the agent.
type SetupStep struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=40
	Name string `json:"name"`

	// +kubebuilder:validation:MinLength=1
	Script string `json:"script"`
}

// CatalogSyncSpec configures operator-managed synchronization of the resolved
// Organization catalog before the resident runtime starts.
type CatalogSyncSpec struct {
	// Enabled adds a dedicated init container that runs outfitter sync with the
	// Agent's settings, workspace, and credential projections.
	Enabled bool `json:"enabled"`
}

// AgentForgeSpec declares the case-preserving login used to route forge events.
type AgentForgeSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+$`
	Username string `json:"username"`
}

// ResourceQuotaSpec contains the aggregate namespace hard limits.
// +kubebuilder:validation:XValidation:rule="self.hard.size() > 0",message="resourceQuota.hard must not be empty"
type ResourceQuotaSpec struct {
	Hard corev1.ResourceList `json:"hard"`
}

// ContainerLimitSpec contains default requests and limits for workspace Pods.
// +kubebuilder:validation:XValidation:rule="self.defaultRequest.size() >= 2",message="defaultRequest must include cpu and memory"
// +kubebuilder:validation:XValidation:rule="self.default.size() >= 2",message="default must include cpu and memory"
type ContainerLimitSpec struct {
	DefaultRequest corev1.ResourceList `json:"defaultRequest"`

	Default corev1.ResourceList `json:"default"`
}

// WorkspaceLimitRangeSpec defines one container LimitRange item.
type WorkspaceLimitRangeSpec struct {
	Container ContainerLimitSpec `json:"container"`
}

// WorkspaceVolumeSpec configures the durable per-agent cache volume.
type WorkspaceVolumeSpec struct {
	// +kubebuilder:default="10Gi"
	Size resource.Quantity `json:"size,omitempty"`

	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// WorkspaceSpec configures the bounded agent namespace workspace.
type WorkspaceSpec struct {
	ResourceQuota ResourceQuotaSpec       `json:"resourceQuota"`
	LimitRange    WorkspaceLimitRangeSpec `json:"limitRange"`

	// +optional
	Volume WorkspaceVolumeSpec `json:"volume,omitempty"`
}

// AgentSpec defines the desired state of Agent.
type AgentSpec struct {
	// M1 exercises one membership while retaining the eventual list shape.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	// +listType=map
	// +listMapKey=organization
	Memberships []Membership `json:"memberships"`

	// Image selects the user-owned runtime image. When omitted, the operator's
	// configured default image is used.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image,omitempty"`

	Profile AgentProfile `json:"profile"`

	// CredentialSecretName selects the Agent's single standard Secret.
	// +kubebuilder:default="agent-credentials"
	// +kubebuilder:validation:MinLength=1
	CredentialSecretName string `json:"credentialSecretName,omitempty"`

	// Forge declares this resident's identity on its Organization forge.
	// +optional
	Forge *AgentForgeSpec `json:"forge,omitempty"`

	// Channels explicitly selects the resident runtime's channel sources. When
	// omitted, the runtime retains its own source-discovery behavior.
	// +listType=set
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:Pattern=`^[a-z][a-z0-9_-]*$`
	Channels []string `json:"channels,omitempty"`

	// GitHub configures notification routing and cadence for resident agents.
	// +optional
	GitHub *GitHubSpec `json:"github,omitempty"`

	// Browser adds a headless-Chrome DevTools sidecar to the agent Pod.
	// +optional
	Browser *BrowserSpec `json:"browser,omitempty"`

	// NetworkPolicy overrides the Organization network-policy default. When
	// omitted, the Organization setting applies; if both are omitted, the
	// operator leaves Agent Pod networking unmanaged.
	// +optional
	NetworkPolicy *AgentNetworkPolicySpec `json:"networkPolicy,omitempty"`

	// EnvFrom adds Kubernetes-native Secret and ConfigMap environment sources
	// after the standard credential Secret.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Volumes accepts read-only Secret and ConfigMap inputs for the Agent and
	// its setup containers. Other Kubernetes volume sources are rejected.
	// +listType=map
	// +listMapKey=name
	// +optional
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// VolumeMounts attaches declared input volumes to the Agent and setup
	// containers.
	// +listType=map
	// +listMapKey=name
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// CatalogSync asks the operator to synchronize the resolved Organization
	// catalog before user setup steps and the resident runtime start. The sync
	// init container receives the Agent's generic credential projections, but
	// it does not receive the Kubernetes API token.
	// +optional
	CatalogSync *CatalogSyncSpec `json:"catalogSync,omitempty"`

	// Setup steps run as ordered init containers before the agent starts, with
	// the agent's credentials and workspace mounted. Use them for usecase
	// bootstrap (e.g. provisioning a mailbox, waiting for a dependency).
	// +listType=atomic
	// +optional
	Setup []SetupStep `json:"setup,omitempty"`

	Workspace WorkspaceSpec `json:"workspace"`
}

// AgentStatus defines the observed state of Agent.
type AgentStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Namespace string `json:"namespace,omitempty"`

	// +optional
	OutfitterRevision string `json:"outfitterRevision,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +optional
	CatalogSources []CatalogSourceStatus `json:"catalogSources,omitempty"`

	// +optional
	ResolvedImageDigest string `json:"resolvedImageDigest,omitempty"`

	// +optional
	QuotaHard corev1.ResourceList `json:"quotaHard,omitempty"`

	// +optional
	QuotaUsed corev1.ResourceList `json:"quotaUsed,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 57",message="agent name must be no longer than 57 characters"
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=".status.namespace"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Agent is the Schema for the agents API.
type Agent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              AgentSpec `json:"spec"`
	// +optional
	Status AgentStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// AgentList contains a list of Agent.
type AgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Agent `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Agent{}, &AgentList{})
		return nil
	})
}
