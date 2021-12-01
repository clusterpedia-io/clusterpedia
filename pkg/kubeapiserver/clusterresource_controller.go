package kubeapiserver

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"

	clustersv1alpha1 "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusters/v1alpha1"
	clusterinformer "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/clusters/v1alpha1"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/clusters/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
)

type ClusterResourceController struct {
	clusterLister clusterlister.PediaClusterLister

	restManager      *RESTManager
	discoveryManager *discovery.DiscoveryManager

	clusterresources map[string]ResourceInfoMap
}

func NewClusterResourceController(restManager *RESTManager, discoveryManager *discovery.DiscoveryManager, informer clusterinformer.PediaClusterInformer) *ClusterResourceController {
	controller := &ClusterResourceController{
		clusterLister: informer.Lister(),

		restManager:      restManager,
		discoveryManager: discoveryManager,
		clusterresources: make(map[string]ResourceInfoMap),
	}

	informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.updateClusterResources(obj.(*clustersv1alpha1.PediaCluster))
		},
		UpdateFunc: func(_, obj interface{}) {
			cluster := obj.(*clustersv1alpha1.PediaCluster)
			if !cluster.DeletionTimestamp.IsZero() {
				controller.removeCluster(cluster.Name)
				return
			}

			controller.updateClusterResources(obj.(*clustersv1alpha1.PediaCluster))
		},
		DeleteFunc: func(obj interface{}) {
			clusterName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				return
			}

			controller.removeCluster(clusterName)
		},
	})
	return controller
}

func (c *ClusterResourceController) updateClusterResources(cluster *clustersv1alpha1.PediaCluster) {
	resources := ResourceInfoMap{}
	for _, groupstatus := range cluster.Status.Resources {
		for _, resourcestatus := range groupstatus.Resources {
			if len(resourcestatus.SyncConditions) == 0 {
				continue
			}

			versions := sets.NewString()
			for _, cond := range resourcestatus.SyncConditions {
				versions.Insert(cond.Version)
			}

			gr := schema.GroupResource{Group: groupstatus.Group, Resource: resourcestatus.Resource}
			resources[gr] = resourceInfo{
				Namespaced: resourcestatus.Namespaced,
				Kind:       resourcestatus.Kind,
				Versions:   versions,
			}
		}
	}

	clusterresources := c.clusterresources[cluster.Name]
	if reflect.DeepEqual(resources, clusterresources) {
		return
	}

	discoveryapis := c.restManager.LoadResources(resources)
	c.discoveryManager.SetClusterGroupResource(cluster.Name, discoveryapis)

	c.clusterresources[cluster.Name] = resources
}

func (c *ClusterResourceController) removeCluster(name string) {
	if _, ok := c.clusterresources[name]; !ok {
		return
	}

	c.discoveryManager.RemoveCluster(name)
	delete(c.clusterresources, name)
}

type resourceInfo struct {
	Namespaced bool
	Kind       string
	Versions   sets.String
}

type ResourceInfoMap map[schema.GroupResource]resourceInfo
