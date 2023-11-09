package storageconfig

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	"k8s.io/apiserver/pkg/server/resourceconfig"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
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

	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(scheme.LegacyResourceScheme)
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

func (g *StorageConfigFactory) NewConfig(gvr schema.GroupVersionResource, namespaced bool, kind string) (*storage.ResourceStorageConfig, error) {
	if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
		return g.NewLegacyResourceConfig(gvr.GroupResource(), namespaced, kind)
	}
	return g.NewUnstructuredConfig(gvr, namespaced, kind)
}

func (g *StorageConfigFactory) NewUnstructuredConfig(gvr schema.GroupVersionResource, namespaced bool, kind string) (*storage.ResourceStorageConfig, error) {
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

	newFunc := func() runtime.Object {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(version.WithKind(kind))
		return obj
	}

	return &storage.ResourceStorageConfig{
		GroupResource:        gvr.GroupResource(),
		StorageGroupResource: gvr.GroupResource(),
		Codec:                codec,
		StorageVersion:       version,
		MemoryVersion:        version,
		Namespaced:           namespaced,
		NewFunc:              newFunc,
	}, nil
}

func (g *StorageConfigFactory) NewLegacyResourceConfig(gr schema.GroupResource, namespaced bool, kind string) (*storage.ResourceStorageConfig, error) {
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
		StorageSerializer: scheme.LegacyResourceCodecs,
		MemoryVersion:     memoryVersion,
		StorageVersion:    storageVersion,
	}
	codec, _, err := serverstorage.NewStorageCodec(codecConfig)
	if err != nil {
		return nil, err
	}

	newFunc := func() runtime.Object {
		obj, _ := scheme.LegacyResourceScheme.New(memoryVersion.WithKind(kind))
		return obj
	}

	return &storage.ResourceStorageConfig{
		GroupResource:        gr,
		StorageGroupResource: chosenStorageResource,
		Codec:                codec,
		StorageVersion:       codecConfig.StorageVersion,
		MemoryVersion:        memoryVersion,
		Namespaced:           namespaced,
		NewFunc:              newFunc,
	}, nil
}
