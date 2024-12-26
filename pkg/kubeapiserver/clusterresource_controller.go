package kubeapiserver

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	clusterinformer "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions/cluster/v1alpha2"
	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
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

	if _, err := informer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.updateClusterResources(obj.(*clusterv1alpha2.PediaCluster))
		},
		UpdateFunc: func(_, obj interface{}) {
			cluster := obj.(*clusterv1alpha2.PediaCluster)
			if !cluster.DeletionTimestamp.IsZero() {
				controller.removeCluster(cluster.Name)
				return
			}

			controller.updateClusterResources(obj.(*clusterv1alpha2.PediaCluster))
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

func (c *ClusterResourceController) updateClusterResources(cluster *clusterv1alpha2.PediaCluster) {
	resources := ResourceInfoMap{}
	for _, groupResources := range cluster.Status.SyncResources {
		for _, resource := range groupResources.Resources {
			if len(resource.SyncConditions) == 0 {
				continue
			}

			versions := sets.Set[string]{}
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

func (c *ClusterResourceController) resolveClusterRestConfig(name string) (*rest.Config, error) {
	cluster, err := c.clusterLister.Get(name)
	if err != nil {
		return nil, err
	}

	if len(cluster.Spec.Kubeconfig) != 0 {
		clientconfig, err := clientcmd.NewClientConfigFromBytes(cluster.Spec.Kubeconfig)
		if err != nil {
			return nil, err
		}
		return clientconfig.ClientConfig()
	}

	if cluster.Spec.APIServer == "" {
		return nil, errors.New("Cluster APIServer Endpoint is required")
	}

	if len(cluster.Spec.TokenData) == 0 &&
		(len(cluster.Spec.CertData) == 0 || len(cluster.Spec.KeyData) == 0) {
		return nil, errors.New("Cluster APIServer's Token or Cert is required")
	}

	config := &rest.Config{
		Host: cluster.Spec.APIServer,
	}

	if len(cluster.Spec.CAData) != 0 {
		config.TLSClientConfig.CAData = cluster.Spec.CAData
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if len(cluster.Spec.CertData) != 0 && len(cluster.Spec.KeyData) != 0 {
		config.TLSClientConfig.CertData = cluster.Spec.CertData
		config.TLSClientConfig.KeyData = cluster.Spec.KeyData
	}

	if len(cluster.Spec.TokenData) != 0 {
		config.BearerToken = string(cluster.Spec.TokenData)
	}
	return config, nil
}

func (c *ClusterResourceController) GetClusterDefaultConnection(ctx context.Context, name string) (string, http.RoundTripper, error) {
	config, err := c.resolveClusterRestConfig(name)
	if err != nil {
		return "", nil, err
	}
	transport, err := rest.TransportFor(config)
	if err != nil {
		return "", nil, err
	}
	return config.Host, transport, nil
}

func (c *ClusterResourceController) GetClusterConnectionWithTLSConfig(ctx context.Context, name string) (string, http.RoundTripper, error) {
	return "", nil, errors.New("CetClusterConnectionWithTLSConfig not implemented")
}

type resourceInfo struct {
	Namespaced bool
	Kind       string
	Versions   sets.Set[string]
}

type ResourceInfoMap map[schema.GroupResource]resourceInfo
