package clustersynchro

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
)

type CustomResourceController struct {
	lock      sync.RWMutex
	versions  map[schema.GroupResource][]string
	resources map[schema.GroupResource]*meta.RESTMapping

	informer                cache.SharedIndexInformer
	resourceMutationHandler func()
}

func NewCustomResourceController(cluster string, config *rest.Config, version string) (*CustomResourceController, error) {
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

	lw := cache.NewListWatchFromClient(getter, "customresourcedefinitions", metav1.NamespaceNone, fields.Nothing())
	controller := &CustomResourceController{
		resources: make(map[schema.GroupResource]*meta.RESTMapping),
		versions:  make(map[schema.GroupResource][]string),
		informer:  cache.NewSharedIndexInformer(lw, exampleObject, 0, nil),
	}
	controller.informer.AddEventHandler(controller)
	return controller, nil
}

func (c *CustomResourceController) Run(stopCh <-chan struct{}) {
	c.informer.Run(stopCh)
}

func (c *CustomResourceController) HasSynced() bool {
	return c.informer.HasSynced()
}

func (c *CustomResourceController) SetResourceMutationHandler(handler func()) {
	// TODO: race
	c.resourceMutationHandler = handler
}

func (c *CustomResourceController) OnAdd(obj interface{}) {
	c.updateResources(obj)
}

func (c *CustomResourceController) OnUpdate(_, obj interface{}) {
	c.updateResources(obj)
}

func (c *CustomResourceController) OnDelete(obj interface{}) {
	c.removeResource(obj)
}

func (c *CustomResourceController) updateResources(obj interface{}) {
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
	mapping := &meta.RESTMapping{
		Resource: groupResource.WithVersion(""),
		GroupVersionKind: schema.GroupVersionKind{
			Group:   crd.Spec.Group,
			Version: "",
			Kind:    crd.Spec.Names.Kind,
		},
	}
	switch crd.Spec.Scope {
	case apiextensionsv1.ClusterScoped:
		mapping.Scope = meta.RESTScopeRoot
	case apiextensionsv1.NamespaceScoped:
		mapping.Scope = meta.RESTScopeNamespace
	}

	versions := make([]string, 0, len(crd.Spec.Versions))
	for _, version := range crd.Spec.Versions {
		if version.Served {
			versions = append(versions, version.Name)
		}
	}
	sortVersionByKubeAwareVersion(versions)

	c.lock.Lock()
	if reflect.DeepEqual(c.versions[groupResource], versions) &&
		reflect.DeepEqual(c.resources[groupResource], mapping) {
		c.lock.Unlock()
		return
	}

	c.versions[groupResource] = versions
	c.resources[groupResource] = mapping
	c.lock.Unlock()

	if c.resourceMutationHandler != nil {
		c.resourceMutationHandler()
	}
}

func sortVersionByKubeAwareVersion(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return version.CompareKubeAwareVersionStrings(versions[i], versions[j]) > 0
	})
}

func (c *CustomResourceController) removeResource(obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if tombstone.Obj == nil {
			klog.Errorf("Couldn't get object from tombstone %#v", obj)
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
		klog.Errorf("object that is not expected %#v", obj)
		return
	}
	crd, ok := runtimeobj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return
	}

	groupResource := schema.GroupResource{Group: crd.Spec.Group, Resource: crd.Status.AcceptedNames.Plural}
	c.lock.Lock()
	delete(c.versions, groupResource)
	delete(c.resources, groupResource)
	c.lock.Unlock()

	if c.resourceMutationHandler != nil {
		c.resourceMutationHandler()
	}
}

func (c *CustomResourceController) GetRESTMappingAndVersions(gr schema.GroupResource) (*meta.RESTMapping, []string) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.resources[gr], c.versions[gr]
}
