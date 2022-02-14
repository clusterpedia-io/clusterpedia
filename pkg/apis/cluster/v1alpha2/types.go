package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ClusterConditionReady = "Ready"

	SyncStatusPending = "Pending"
	SyncStatusSyncing = "Syncing"
	SyncStatusStop    = "Stop"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:printcolumn:name="APIServer",type=string,JSONPath=".spec.apiserver"
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=".status.version"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=".status.conditions[?(@.type == 'Ready')].reason"
type PediaCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterSpec `json:"spec,omitempty"`

	// +optional
	Status ClusterStatus `json:"status,omitempty"`
}

type ClusterSpec struct {
	// +required
	// +kubebuilder:validation:Required
	APIServer string `json:"apiserver"`

	// +optional
	TokenData []byte `json:"tokenData,omitempty"`

	// +optional
	CAData []byte `json:"caData,omitempty"`

	// +optional
	CertData []byte `json:"certData,omitempty"`

	// +optional
	KeyData []byte `json:"keyData,omitempty"`

	// +required
	SyncResources []ClusterGroupResources `json:"syncResources"`
}

type ClusterGroupResources struct {
	Group string `json:"group"`

	// +optional
	Versions []string `json:"versions,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Resources []string `json:"resources"`
}

type ClusterStatus struct {
	// +required
	// +kubebuilder:validation:Required
	Version string `json:"version,omitempty"`

	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	SyncResources []ClusterGroupResourcesStatus `json:"syncResources,omitempty"`
}

type ClusterGroupResourcesStatus struct {
	// +required
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// +required
	// +kubebuilder:validation:Required
	Resources []ClusterResourceStatus `json:"resources"`
}

type ClusterResourceStatus struct {
	// +required
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// +required
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// +required
	// +kubebuilder:validation:Required
	Namespaced bool `json:"namespaced"`

	// +required
	// +kubebuilder:validation:Required
	SyncConditions []ClusterResourceSyncCondition `json:"syncConditions"`
}

type ClusterResourceSyncCondition struct {
	// +required
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// optional
	StorageVersion string `json:"storageVersion,omitempty"`

	// optional
	StorageResource *string `json:"storrageResource,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	Status string `json:"status"`

	// optional
	Reason string `json:"reason,omitempty"`

	// optional
	Message string `json:"message,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Format=date-time
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type PediaClusterList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PediaCluster `json:"items"`
}
