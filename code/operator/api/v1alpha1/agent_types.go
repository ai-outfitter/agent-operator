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
	AgentConditionWorkspaceReady         = "WorkspaceReady"
	AgentConditionCredentialsReady       = "CredentialsReady"
	AgentConditionOutfitterSettingsReady = "OutfitterSettingsReady"
	AgentConditionWorkloadReady          = "WorkloadReady"
	AgentConditionReady                  = "Ready"
)

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

// CredentialExposure controls how a referenced object reaches the runtime.
// +kubebuilder:validation:Enum=env;volume
type CredentialExposure string

const (
	CredentialExposureEnv    CredentialExposure = "env"
	CredentialExposureVolume CredentialExposure = "volume"
)

// CredentialReference identifies one object by name only. The operator checks
// existence but never reads or validates object contents.
// +kubebuilder:validation:XValidation:rule="has(self.secret) != has(self.configMap)",message="exactly one of secret or configMap must be set"
type CredentialReference struct {
	// +optional
	// +kubebuilder:validation:MinLength=1
	Secret *string `json:"secret,omitempty"`

	// +optional
	// +kubebuilder:validation:MinLength=1
	ConfigMap *string `json:"configMap,omitempty"`

	As CredentialExposure `json:"as"`
}

// SetupStep defines one initialization script that runs before the agent.
type SetupStep struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=40
	Name string `json:"name"`

	// +kubebuilder:validation:MinLength=1
	Script string `json:"script"`
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

	// +listType=atomic
	// +optional
	Credentials []CredentialReference `json:"credentials,omitempty"`

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
