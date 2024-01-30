package kubeapiserver

import (
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	clusterinformer "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/cluster/v1alpha2"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	watchcomponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
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

	if _, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.updateClusterResources(obj.(*clusterv1alpha2.PediaCluster))
		},
		UpdateFunc: func(oldObj, obj interface{}) {
			cluster := obj.(*clusterv1alpha2.PediaCluster)
			if !cluster.DeletionTimestamp.IsZero() {
				controller.clearCache(cluster)
				controller.removeCluster(cluster.Name)
				return
			}

			controller.updateCache(oldObj.(*clusterv1alpha2.PediaCluster), cluster)
			controller.updateClusterResources(cluster)
		},
		DeleteFunc: func(obj interface{}) {
			clusterName, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err != nil {
				return
			}

			controller.removeCluster(clusterName)
		},
	}); err != nil {
		klog.ErrorS(err, "error when adding event handler to informer")
	}

	return controller
}

func (c *ClusterResourceController) convertCluster2Map(cluster *clusterv1alpha2.PediaCluster) ResourceInfoMap {
	resources := ResourceInfoMap{}
	for _, groupResources := range cluster.Status.SyncResources {
		for _, resource := range groupResources.Resources {
			if len(resource.SyncConditions) == 0 {
				continue
			}

			versions := sets.New[string]()
			for _, cond := range resource.SyncConditions {
				versions.Insert(cond.Version)
			}

			gr := schema.GroupResource{Group: groupResources.Group, Resource: resource.Name}
			resources[gr] = resourceInfo{
				Namespaced: resource.Namespaced,
				Kind:       resource.Kind,
				Versions:   versions,
			}
		}
	}

	return resources
}

func (c *ClusterResourceController) updateCache(oldCluster *clusterv1alpha2.PediaCluster, cluster *clusterv1alpha2.PediaCluster) {
	if ecp := watchcomponents.GetInitEventCachePool(); ecp == nil {
		return
	}
	oldResources := c.convertCluster2Map(oldCluster)
	resources := c.convertCluster2Map(cluster)
	for gr, ri := range oldResources {
		for version := range ri.Versions {
			if !resources[gr].Versions.Has(version) {
				// gr has deleted, clear the cache of this gv
				watchcomponents.GetInitEventCachePool().ClearCacheByGVR(schema.GroupVersionResource{
					Group: gr.Group, Version: version, Resource: gr.Resource,
				})
			}
		}
	}
}

func (c *ClusterResourceController) clearCache(cluster *clusterv1alpha2.PediaCluster) {
	if ecp := watchcomponents.GetInitEventCachePool(); ecp == nil {
		return
	}
	currentResources := c.clusterresources[cluster.Name]
	resources := c.convertCluster2Map(cluster)

	for gr, ri := range currentResources {
		for version := range ri.Versions {
			// clear the cache of this gv which in cluster
			if resources[gr].Versions.Has(version) {
				watchcomponents.GetInitEventCachePool().ClearCacheByGVR(schema.GroupVersionResource{
					Group: gr.Group, Version: version, Resource: gr.Resource,
				})
			}
		}
	}
}

func (c *ClusterResourceController) updateClusterResources(cluster *clusterv1alpha2.PediaCluster) {
	resources := c.convertCluster2Map(cluster)

	currentResources := c.clusterresources[cluster.Name]
	if reflect.DeepEqual(resources, currentResources) {
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
	Versions   sets.Set[string]
}

type ResourceInfoMap map[schema.GroupResource]resourceInfo
