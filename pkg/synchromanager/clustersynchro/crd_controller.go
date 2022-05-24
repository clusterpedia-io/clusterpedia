package clustersynchro

import (
	"errors"
	"fmt"
	"sort"

	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/discovery"
)

type CRDController struct {
	cluster        string
	informer       cache.SharedIndexInformer
	discoveryCache discovery.CustomResourceCache
}

func NewCRDController(cluster string, config *rest.Config, version string, discoveryCache discovery.CustomResourceCache) (*CRDController, error) {
	client, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	var getter cache.Getter
	var exampleObject runtime.Object
	switch version {
	case apiextensionsv1.SchemeGroupVersion.Version:
		getter = client.ApiextensionsV1().RESTClient()
		exampleObject = &apiextensionsv1.CustomResourceDefinition{}
	case apiextensionsv1beta1.SchemeGroupVersion.Version:
		getter = client.ApiextensionsV1beta1().RESTClient()
		exampleObject = &apiextensionsv1beta1.CustomResourceDefinition{}
	default:
		return nil, fmt.Errorf("CRD Version %s is not supported", version)
	}

	lw := cache.NewListWatchFromClient(getter, "customresourcedefinitions", metav1.NamespaceNone, fields.Everything())
	controller := &CRDController{
		cluster:        cluster,
		informer:       cache.NewSharedIndexInformer(lw, exampleObject, 0, nil),
		discoveryCache: discoveryCache,
	}
	controller.informer.AddEventHandler(controller)
	return controller, nil
}

func (c *CRDController) Run(stopCh <-chan struct{}) {
	c.informer.Run(stopCh)
}

func (c *CRDController) HasSynced() bool {
	return c.informer.HasSynced()
}

func (c *CRDController) OnAdd(obj interface{}) {
	c.updateResources(obj)
}

func (c *CRDController) OnUpdate(_, obj interface{}) {
	c.updateResources(obj)
}

func (c *CRDController) OnDelete(obj interface{}) {
	c.removeResource(obj)
}

func (c *CRDController) updateResources(obj interface{}) {
	runtimeobj, err := resourcescheme.LegacyResourceScheme.ConvertToVersion(obj.(runtime.Object), apiextensionsv1.SchemeGroupVersion)
	if err != nil {
		return
	}
	crd, ok := runtimeobj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return
	}
	if !apiextensionshelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established) {
		// keep the resource and versions already set
		return
	}

	groupResource := schema.GroupResource{Group: crd.Spec.Group, Resource: crd.Status.AcceptedNames.Plural}
	apiResource := metav1.APIResource{
		Name:         crd.Status.AcceptedNames.Plural,
		SingularName: crd.Status.AcceptedNames.Singular,
		Kind:         crd.Status.AcceptedNames.Kind,
		ShortNames:   crd.Spec.Names.ShortNames,
		Categories:   crd.Spec.Names.Categories,
		Verbs:        metav1.Verbs([]string{"get", "list", "watch"}),
		Namespaced:   crd.Spec.Scope == apiextensionsv1.NamespaceScoped,
	}

	versions := make([]string, 0, len(crd.Spec.Versions))
	for _, version := range crd.Spec.Versions {
		if version.Served {
			versions = append(versions, version.Name)
		}
	}
	sortVersionByKubeAwareVersion(versions)

	c.discoveryCache.UpdateCustomResource(groupResource, apiResource, versions)
}

func (c *CRDController) removeResource(obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if tombstone.Obj == nil {
			klog.ErrorS(errors.New("Couldn't get object from tombstone"), "cluster", c.cluster, "object", obj)
			return
		}
		obj = tombstone.Obj
	}

	runtimeobj, ok := obj.(runtime.Object)
	if !ok {
		return
	}
	runtimeobj, err := resourcescheme.LegacyResourceScheme.ConvertToVersion(runtimeobj, apiextensionsv1.SchemeGroupVersion)
	if err != nil {
		klog.ErrorS(errors.New("object that is not expected"), "cluster", c.cluster, "object", obj)
		return
	}
	crd, ok := runtimeobj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return
	}

	groupResource := schema.GroupResource{Group: crd.Spec.Group, Resource: crd.Status.AcceptedNames.Plural}
	c.discoveryCache.RemoveCustomResource(groupResource)
}

func sortVersionByKubeAwareVersion(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return version.CompareKubeAwareVersionStrings(versions[i], versions[j]) > 0
	})
}
