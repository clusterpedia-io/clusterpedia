package pedia

import (
	"net/url"

	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	SearchLabelOwner      = "search.clusterpedia.io/owner"
	SearchLabelNames      = "search.clusterpedia.io/names"
	SearchLabelClusters   = "search.clusterpedia.io/clusters"
	SearchLabelNamespaces = "search.clusterpedia.io/namespaces"
	SearchLabelOrderBy    = "search.clusterpedia.io/orderby"

	SearchLabelSize   = "search.clusterpedia.io/size"
	SearchLabelOffset = "search.clusterpedia.io/offset"

	ShadowLabelClusterName          = "shadow.clusterpedia.io/cluster-name"
	ShadowLabelGroupVersionResource = "shadow.clusterpedia.io/gvr"
)

type OrderBy struct {
	Field string
	Desc  bool
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ListOptions struct {
	metainternal.ListOptions

	Names        []string
	Owner        string
	ClusterNames []string
	Namespaces   []string
	OrderBy      []OrderBy

	// +k8s:conversion-fn:drop
	ExtraLabelSelector labels.Selector

	// +k8s:conversion-fn:drop
	ExtraQuery url.Values

	// RelatedResources []schema.GroupVersionKind
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CollectionResource struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	ResourceTypes []CollectionResourceType
	Items         []runtime.Object
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CollectionResourceList struct {
	metav1.TypeMeta
	metav1.ListMeta

	Items []CollectionResource
}

type CollectionResourceType struct {
	Group    string
	Version  string
	Kind     string
	Resource string
}

func (t CollectionResourceType) GroupResource() schema.GroupResource {
	return schema.GroupResource{
		Group:    t.Group,
		Resource: t.Resource,
	}
}
