package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// WorkspaceStoragePolicy selects how long a workspace keeps its files.
// +kubebuilder:validation:Enum=reusable;task
type WorkspaceStoragePolicy string

const (
	// StoragePolicyReusable keeps files across idle sleeps until the user-renewed expiry.
	StoragePolicyReusable WorkspaceStoragePolicy = "reusable"
	// StoragePolicyTask runs one submitted task; the gateway fixes expiresAt once it finishes.
	StoragePolicyTask WorkspaceStoragePolicy = "task"
)

// EphemeralWorkspaceSpec is independent of resident Agent identity and catalogs.
// +kubebuilder:validation:XValidation:rule="self.awakeUntil <= self.expiresAt",message="awakeUntil must not exceed expiresAt"
// +kubebuilder:validation:XValidation:rule="self.computeGeneration >= oldSelf.computeGeneration",message="computeGeneration cannot decrease"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.storagePolicy) || self.storagePolicy == oldSelf.storagePolicy",message="storagePolicy is immutable"
type EphemeralWorkspaceSpec struct {
	// CredentialSecretName identifies the workspace-scoped token; never an upstream credential.
	// +kubebuilder:validation:Pattern=`^ws-[a-z0-9-]+$`
	CredentialSecretName string `json:"credentialSecretName"`
	// AwakeUntil is renewed by the trusted gateway while a bounded run is active.
	AwakeUntil metav1.Time `json:"awakeUntil"`
	// ExpiresAt is renewed only by user activity. Expiry removes this workspace, not its namespace.
	ExpiresAt metav1.Time `json:"expiresAt"`
	// +kubebuilder:default="5Gi"
	// +kubebuilder:validation:Pattern=`^[1-9][0-9]*(Mi|Gi)$`
	StorageSize string `json:"storageSize,omitempty"`
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
	// StoragePolicy is reusable (idle sleep plus user-renewed expiry) or task (one submitted run,
	// retained for a fixed period after it finishes). The gateway enforces the task limits.
	// +kubebuilder:default=reusable
	// +optional
	StoragePolicy WorkspaceStoragePolicy `json:"storagePolicy,omitempty"`
	// ComputeGeneration selects the current runtime Pod. The gateway sets 1 at creation and increments
	// it for every normal wake from Sleeping; active-run renewals leave it unchanged.
	// +kubebuilder:validation:Minimum=1
	ComputeGeneration int64 `json:"computeGeneration"`
	// Image is a digest-pinned runtime reference chosen by trusted operator configuration. When unset
	// the controller default applies. Either is captured when a generation's Pod is created and never
	// changes an existing Pod.
	// +kubebuilder:validation:Pattern=`^[^@\s]+@sha256:[a-f0-9]{64}$`
	// +optional
	Image string `json:"image,omitempty"`
}

type WorkspaceStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// ComputeGeneration is the last generation for which the controller attempted a runtime Pod.
	// +optional
	ComputeGeneration int64 `json:"computeGeneration,omitempty"`
	// PodName is the runtime Pod of the current generation; cleared before the controller stops it.
	// +optional
	PodName string `json:"podName,omitempty"`
	// ResolvedImage is the exact reference the current or last runtime Pod was created with.
	// +optional
	ResolvedImage string `json:"resolvedImage,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].reason"
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=".spec.storagePolicy"
// +kubebuilder:printcolumn:name="Generation",type=integer,JSONPath=".status.computeGeneration"
// Workspace is a temporary environment. It has no resident identity or Kubernetes API authority.
type Workspace struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              EphemeralWorkspaceSpec `json:"spec"`
	// +optional
	Status WorkspaceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true
type WorkspaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Workspace `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Workspace{}, &WorkspaceList{})
		return nil
	})
}
