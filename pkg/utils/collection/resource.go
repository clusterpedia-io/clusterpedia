package collection

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
)

const (
	CollectionResourceAny           = "any"
	CollectionResourceWorkloads     = "workloads"
	CollectionResourceKubeResources = "kuberesources"
)

var CollectionResources = []internal.CollectionResource{
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: CollectionResourceAny,
		},
		ResourceTypes: []internal.CollectionResourceType{},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: CollectionResourceWorkloads,
		},
		ResourceTypes: []internal.CollectionResourceType{
			{
				Group:    "apps",
				Resource: "deployments",
			},
			{
				Group:    "apps",
				Resource: "daemonsets",
			},
			{
				Group:    "apps",
				Resource: "statefulsets",
			},
		},
	},
	{
		ObjectMeta: metav1.ObjectMeta{
			Name: CollectionResourceKubeResources,
		},
	},
}

func init() {
	groups := sets.NewString()
	for _, groupversion := range resourcescheme.LegacyResourceScheme.PreferredVersionAllGroups() {
		groups.Insert(groupversion.Group)
	}

	types := make([]internal.CollectionResourceType, 0, len(groups))
	for _, group := range groups.List() {
		types = append(types, internal.CollectionResourceType{
			Group: group,
		})
	}

	for i := range CollectionResources {
		if CollectionResources[i].Name == CollectionResourceKubeResources {
			CollectionResources[i].ResourceTypes = types
		}
	}
}
