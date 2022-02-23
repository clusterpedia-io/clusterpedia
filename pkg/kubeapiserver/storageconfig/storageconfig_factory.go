package storageconfig

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	"k8s.io/apiserver/pkg/server/resourceconfig"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type StorageConfigFactory struct {
	legacyResourceEncodingConfig serverstorage.ResourceEncodingConfig

	cohabitatingResources map[schema.GroupResource][]schema.GroupResource
}

func NewStorageConfigFactory() *StorageConfigFactory {
	resources := []schema.GroupVersionResource{
		apisstorage.Resource("csistoragecapacities").WithVersion("v1beta1"),
	}

	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(resourcescheme.LegacyResourceScheme)
	resourceEncodingConfig = resourceconfig.MergeResourceEncodingConfigs(resourceEncodingConfig, resources)

	factory := &StorageConfigFactory{
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
func (f *StorageConfigFactory) addCohabitatingResources(groupResources ...schema.GroupResource) {
	for _, groupResource := range groupResources {
		f.cohabitatingResources[groupResource] = groupResources
	}
}
*/

func (g *StorageConfigFactory) GetStorageGroupResource(groupResource schema.GroupResource) schema.GroupResource {
	if len(g.cohabitatingResources[groupResource]) != 0 {
		return g.cohabitatingResources[groupResource][0]
	}
	return groupResource
}

func (g *StorageConfigFactory) NewConfig(gvr schema.GroupVersionResource) (*storage.ResourceStorageConfig, error) {
	if resourcescheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
		return g.NewLegacyResourceConfig(gvr.GroupResource())
	}
	return g.NewCustomResourceConfig(gvr)
}

func (g *StorageConfigFactory) NewCustomResourceConfig(gvr schema.GroupVersionResource) (*storage.ResourceStorageConfig, error) {
	version := gvr.GroupVersion()
	codec := versioning.NewCodec(
		resourcescheme.CustomResourceCodecs,
		resourcescheme.CustomResourceCodecs,
		resourcescheme.CustomResourceScheme,
		resourcescheme.CustomResourceScheme,
		resourcescheme.CustomResourceScheme,
		nil,
		version,
		version,
		"customResourceStorage",
	)
	return &storage.ResourceStorageConfig{
		GroupResource:        gvr.GroupResource(),
		StorageGroupResource: gvr.GroupResource(),
		Codec:                codec,
		StorageVersion:       version,
		MemoryVersion:        version,
	}, nil
}

func (g *StorageConfigFactory) NewLegacyResourceConfig(gr schema.GroupResource) (*storage.ResourceStorageConfig, error) {
	chosenStorageResource := g.GetStorageGroupResource(gr)

	storageVersion, err := g.legacyResourceEncodingConfig.StorageEncodingFor(chosenStorageResource)
	if err != nil {
		return nil, err
	}
	memoryVersion, err := g.legacyResourceEncodingConfig.InMemoryEncodingFor(chosenStorageResource)
	if err != nil {
		return nil, err
	}

	codecConfig := serverstorage.StorageCodecConfig{
		StorageMediaType:  runtime.ContentTypeJSON,
		StorageSerializer: resourcescheme.LegacyResourceCodecs,
		MemoryVersion:     memoryVersion,
		StorageVersion:    storageVersion,
	}
	codec, _, err := serverstorage.NewStorageCodec(codecConfig)
	if err != nil {
		return nil, err
	}

	return &storage.ResourceStorageConfig{
		GroupResource:        gr,
		StorageGroupResource: chosenStorageResource,
		Codec:                codec,
		StorageVersion:       codecConfig.StorageVersion,
		MemoryVersion:        memoryVersion,
	}, nil
}
