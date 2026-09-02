package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	OrganizationFinalizer = "organizations.aioutfitter.com/finalizer"

	OrganizationConditionAccepted            = "Accepted"
	OrganizationConditionCatalogSourcesReady = "CatalogSourcesReady"
	OrganizationConditionForgeGatewayReady   = "ForgeGatewayReady"
	OrganizationConditionForgeRoutesReady    = "ForgeRoutesReady"
	OrganizationConditionReady               = "Ready"
)

// ForgeSpoolSpec configures the durable SQLite delivery spool.
type ForgeSpoolSpec struct {
	// +kubebuilder:default="1Gi"
	Size resource.Quantity `json:"size,omitempty"`
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// OrganizationForgeSpec configures one isolated organization gateway.
type OrganizationForgeSpec struct {
	// +kubebuilder:validation:Enum=forgejo
	Provider string `json:"provider"`
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`
	// +kubebuilder:validation:Format=uri
	ServerURL string         `json:"serverURL"`
	Spool     ForgeSpoolSpec `json:"spool"`
}

// Repository declares a generic Git repository owned by an organization.
type Repository struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// URI is a cloneable Git URI. Credential-bearing URIs are rejected by the
	// controller so they cannot leak into status or events.
	// +kubebuilder:validation:MinLength=1
	URI string `json:"uri"`

	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// +optional
	Subdirectory string `json:"subdirectory,omitempty"`

	// Revision optionally pins the initial checkout to an immutable commit.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9a-fA-F]{40}$`
	Revision string `json:"revision,omitempty"`
}

// AgentCatalog declares one commit-pinned source for Outfitter settings.
// Exactly one of github, uri, or localPath identifies the source. LocalPath is
// reserved for development fixtures and does not require a revision.
// +kubebuilder:validation:XValidation:rule="(has(self.github) ? 1 : 0) + (has(self.uri) ? 1 : 0) + (has(self.localPath) ? 1 : 0) == 1",message="exactly one of github, uri, or localPath must be set"
// +kubebuilder:validation:XValidation:rule="has(self.localPath) || has(self.revision)",message="remote catalogs must include a revision"
type AgentCatalog struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// GitHub is an owner/repository shorthand.
	// +optional
	// +kubebuilder:validation:Pattern=`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`
	GitHub *string `json:"github,omitempty"`

	// URI is a cloneable Git URI.
	// +optional
	// +kubebuilder:validation:MinLength=1
	URI *string `json:"uri,omitempty"`

	// LocalPath is an operator-local development fixture path.
	// +optional
	// +kubebuilder:validation:MinLength=1
	LocalPath *string `json:"localPath,omitempty"`

	// Revision is the full immutable commit SHA for a remote source.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-9a-fA-F]{40}$`
	Revision *string `json:"revision,omitempty"`

	// Path selects the Dotagents payload directory within the source.
	// +optional
	Path string `json:"path,omitempty"`
}

// Project is an organization-owned grouping retained in the v1alpha1 schema.
// M1 validates membership references but does not materialize project workloads.
type Project struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +optional
	Repositories []Repository `json:"repositories,omitempty"`
}

// OrganizationSpec defines the desired state of Organization.
type OrganizationSpec struct {
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// CredentialSecretName selects the Organization's single standard Secret.
	// Keys prefixed with default. are inherited by member Agents.
	// +kubebuilder:default="organization-credentials"
	// +kubebuilder:validation:MinLength=1
	CredentialSecretName string `json:"credentialSecretName,omitempty"`

	// Forge configures signed event delivery to Organization members.
	// +optional
	Forge *OrganizationForgeSpec `json:"forge,omitempty"`

	// NetworkPolicy supplies the default for member Agents that do not declare
	// their own setting. When omitted, Agent Pod networking is unmanaged.
	// +optional
	NetworkPolicy *AgentNetworkPolicySpec `json:"networkPolicy,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +optional
	Repositories []Repository `json:"repositories,omitempty"`

	// M1 passes exactly one source to Outfitter while retaining the list shape.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=1
	// +listType=map
	// +listMapKey=name
	AgentCatalogs []AgentCatalog `json:"agentCatalogs"`

	// +listType=map
	// +listMapKey=name
	// +optional
	Projects []Project `json:"projects,omitempty"`
}

// ResolvedRepositoryStatus identifies a repository without copying its URI.
type ResolvedRepositoryStatus struct {
	Name string `json:"name"`

	// +optional
	Revision string `json:"revision,omitempty"`
}

// CatalogSourceStatus identifies a delegated source without copying its URI.
type CatalogSourceStatus struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

// OrganizationStatus defines the observed state of Organization.
type OrganizationStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +optional
	ResolvedRepositories []ResolvedRepositoryStatus `json:"resolvedRepositories,omitempty"`

	// +listType=map
	// +listMapKey=name
	// +optional
	CatalogSources []CatalogSourceStatus `json:"catalogSources,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=".metadata.creationTimestamp"

// Organization is the Schema for the organizations API.
type Organization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              OrganizationSpec `json:"spec"`
	// +optional
	Status OrganizationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// OrganizationList contains a list of Organization.
type OrganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Organization `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Organization{}, &OrganizationList{})
		return nil
	})
}
