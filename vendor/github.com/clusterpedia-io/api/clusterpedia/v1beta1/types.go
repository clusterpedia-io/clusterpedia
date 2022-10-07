package v1beta1

import (
	"net/url"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +k8s:conversion-gen:explicit-from=net/url.Values
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ListOptions struct {
	metav1.ListOptions `json:",inline"`

	// +optional
	Names string `json:"names,omitempty"`

	// +optional
	ClusterNames string `json:"clusters,omitempty"`

	// +optional
	Namespaces string `json:"namespaces,omitempty"`

	// +optional
	OrderBy string `json:"orderby,omitempty"`

	// +optional
	OwnerUID string `json:"ownerUID,omitempty"`

	// +optional
	OwnerName string `json:"ownerName,omitempty"`

	// +optional
	Since string `json:"since,omitempty"`

	// +optional
	Before string `json:"before,omitempty"`

	// +optional
	OwnerGroupResource string `json:"ownerGR,omitempty"`

	// +optional
	OwnerSeniority int `json:"ownerSeniority,omitempty"`

	// +optional
	WithContinue *bool `json:"withContinue,omitempty"`

	// +optional
	WithRemainingCount *bool `json:"withRemainingCount,omitempty"`

	// +optional
	OnlyMetadata bool `json:"onlyMetadata,omitempty"`

	urlQuery url.Values
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type Resources struct {
	metav1.TypeMeta `json:",inline"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CollectionResource struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +required
	ResourceTypes []CollectionResourceType `json:"resourceTypes"`

	// +optional
	Items []runtime.RawExtension `json:"items,omitempty"`

	// +optional
	Continue string `json:"continue,omitempty"`

	// +optional
	RemainingItemCount *int64 `json:"remainingItemCount,omitempty"`
}

type CollectionResourceType struct {
	Group string `json:"group"`

	Version string `json:"version"`

	// +optional
	Kind string `json:"kind,omitempty"`

	Resource string `json:"resource"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type CollectionResourceList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []CollectionResource `json:"items"`
}
