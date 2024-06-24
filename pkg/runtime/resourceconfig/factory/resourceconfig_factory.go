package factory

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	serverresourceconfig "k8s.io/apiserver/pkg/server/resourceconfig"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/runtime/resourceconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/runtime/scheme"
)

type ResourceConfigFactory struct {
	legacyResourceEncodingConfig serverstorage.ResourceEncodingConfig

	cohabitatingResources map[schema.GroupResource][]schema.GroupResource
}

func New() *ResourceConfigFactory {
	resources := []schema.GroupVersionResource{
		apisstorage.Resource("csistoragecapacities").WithVersion("v1beta1"),
	}

	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(scheme.LegacyResourceScheme)
	resourceEncodingConfig = serverresourceconfig.MergeResourceEncodingConfigs(resourceEncodingConfig, resources)

	factory := &ResourceConfigFactory{
		legacyResourceEncodingConfig: resourceEncodingConfig,
		cohabitatingResources:        make(map[schema.GroupResource][]schema.GroupResource),
	}

	/*
		factory.addCohabitatingResources(networking.Resource("networkpolicies"), extensions.Resource("networkpolicies"))
		factory.addCohabitatingResources(apps.Resource("deployments"), extensions.Resource("deployments"))
		factory.addCohabitatingResources(apps.Resource("daemonsets"), extensions.Resource("daemonsets"))
		factory.addCohabitatingResources(apps.Resource("replicasets"), extensions.Resource("replicasets"))
		factory.addCohabitatingResources(apicore.Resource("events"), events.Resource("events"))
		factory.addCohabitatingResources(apicore.Resource("replicationcontrollers"), extensions.Resource("replicationcontrollers")) // to make scale subresources equivalent
		factory.addCohabitatingResources(policy.Resource("podsecuritypolicies"), extensions.Resource("podsecuritypolicies"))
		factory.addCohabitatingResources(networking.Resource("ingresses"), extensions.Resource("ingresses"))
	*/
	return factory
}

/*
func (f *ResourceConfigFactory) addCohabitatingResources(groupResources ...schema.GroupResource) {
	for _, groupResource := range groupResources {
		f.cohabitatingResources[groupResource] = groupResources
	}
}
*/

func (g *ResourceConfigFactory) getStorageGroupResource(groupResource schema.GroupResource) schema.GroupResource {
	if len(g.cohabitatingResources[groupResource]) != 0 {
		return g.cohabitatingResources[groupResource][0]
	}
	return groupResource
}

func (g *ResourceConfigFactory) MemoryResource(gvr schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
		return gvr, nil
	}
	gr := gvr.GroupResource()
	memoryVersion, err := g.legacyResourceEncodingConfig.InMemoryEncodingFor(gr)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return gr.WithVersion(memoryVersion.Version), nil
}

func (g *ResourceConfigFactory) StorageResource(gvr schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
		return gvr, nil
	}

	gr := gvr.GroupResource()
	chosenStorageResource := g.getStorageGroupResource(gr)
	storageVersion, err := g.legacyResourceEncodingConfig.StorageEncodingFor(chosenStorageResource)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return chosenStorageResource.WithVersion(storageVersion.Version), nil
}

func (g *ResourceConfigFactory) NewConfig(gvr schema.GroupVersionResource, namespaced bool) (*resourceconfig.ResourceConfig, error) {
	if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
		return g.NewLegacyResourceConfig(gvr.GroupResource(), namespaced)
	}
	return g.NewUnstructuredConfig(gvr, namespaced)
}

func (g *ResourceConfigFactory) NewUnstructuredConfig(gvr schema.GroupVersionResource, namespaced bool) (*resourceconfig.ResourceConfig, error) {
	version := gvr.GroupVersion()
	codec := versioning.NewCodec(
		scheme.UnstructuredCodecs,
		scheme.UnstructuredCodecs,
		scheme.UnstructuredScheme,
		scheme.UnstructuredScheme,
		scheme.UnstructuredScheme,
		nil,
		version,
		version,
		"unstructuredObjectStorage",
	)
	return &resourceconfig.ResourceConfig{
		Namespaced:    namespaced,
		GroupResource: gvr.GroupResource(),

		StorageResource: gvr,
		MemoryResource:  gvr,
		Codec:           codec,
	}, nil
}

func (g *ResourceConfigFactory) NewLegacyResourceConfig(gr schema.GroupResource, namespaced bool) (*resourceconfig.ResourceConfig, error) {
	chosenStorageResource := g.getStorageGroupResource(gr)

	storageVersion, err := g.legacyResourceEncodingConfig.StorageEncodingFor(chosenStorageResource)
	if err != nil {
		return nil, err
	}
	memoryVersion, err := g.legacyResourceEncodingConfig.InMemoryEncodingFor(gr)
	if err != nil {
		return nil, err
	}

	codecConfig := serverstorage.StorageCodecConfig{
		StorageMediaType:  runtime.ContentTypeJSON,
		StorageSerializer: scheme.LegacyResourceCodecs,
		MemoryVersion:     memoryVersion,
		StorageVersion:    storageVersion,
	}
	codec, _, err := serverstorage.NewStorageCodec(codecConfig)
	if err != nil {
		return nil, err
	}

	return &resourceconfig.ResourceConfig{
		Namespaced:    namespaced,
		GroupResource: gr,

		StorageResource: chosenStorageResource.WithVersion(storageVersion.Version),
		MemoryResource:  gr.WithVersion(memoryVersion.Version),
		Codec:           codec,
	}, nil
}
