package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EphemeralWorkspaceSpec is independent of resident Agent identity and catalogs.
// +kubebuilder:validation:XValidation:rule="self.awakeUntil <= self.expiresAt",message="awakeUntil must not exceed expiresAt"
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
}

type WorkspaceStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
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
