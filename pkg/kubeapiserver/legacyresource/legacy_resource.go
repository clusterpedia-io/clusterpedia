package legacyresource

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/server/resourceconfig"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/kubernetes/pkg/api/legacyscheme"
	apicore "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/apis/rbac"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"
	printersinternal "k8s.io/kubernetes/pkg/printers/internalversion"
	printerstorage "k8s.io/kubernetes/pkg/printers/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/printers"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

var (
	Scheme         = legacyscheme.Scheme
	Codecs         = legacyscheme.Codecs
	ParameterCodec = legacyscheme.ParameterCodec
)

func NewEncodingConfig() serverstorage.ResourceEncodingConfig {
	resources := []schema.GroupVersionResource{
		apisstorage.Resource("csistoragecapacities").WithVersion("v1beta1"),
	}

	resourceEncodingConfig := serverstorage.NewDefaultResourceEncodingConfig(Scheme)
	resourceEncodingConfig = resourceconfig.MergeResourceEncodingConfigs(resourceEncodingConfig, resources)
	return resourceEncodingConfig
}

type StorageConfigFactory struct {
	defaultMediaType       string
	serializer             runtime.StorageSerializer
	resourceEncodingConfig serverstorage.ResourceEncodingConfig

	cohabitatingResources map[schema.GroupResource][]schema.GroupResource
}

func NewStorageConfigFactory(defaultMediaType string) *StorageConfigFactory {
	factory := &StorageConfigFactory{
		defaultMediaType:       defaultMediaType,
		serializer:             Codecs,
		resourceEncodingConfig: NewEncodingConfig(),

		cohabitatingResources: make(map[schema.GroupResource][]schema.GroupResource),
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
	/*
		if len(g.cohabitatingResources[groupResource]) != 0 {
			return g.cohabitatingResources[groupResource][0]
		}
	*/
	return groupResource
}

func (g *StorageConfigFactory) NewConfig(gvr schema.GroupVersionResource) (*storage.ResourceStorageConfig, error) {
	chosenStorageResource := g.GetStorageGroupResource(gvr.GroupResource())

	storageVersion, err := g.resourceEncodingConfig.StorageEncodingFor(chosenStorageResource)
	if err != nil {
		return nil, err
	}
	memoryVersion, err := g.resourceEncodingConfig.InMemoryEncodingFor(chosenStorageResource)
	if err != nil {
		return nil, err
	}

	codecConfig := serverstorage.StorageCodecConfig{
		StorageMediaType:  g.defaultMediaType,
		StorageSerializer: g.serializer,
		MemoryVersion:     memoryVersion,
		StorageVersion:    storageVersion,
	}
	codec, _, err := serverstorage.NewStorageCodec(codecConfig)
	if err != nil {
		return nil, err
	}

	return &storage.ResourceStorageConfig{
		GroupResource:        gvr.GroupResource(),
		StorageGroupResource: chosenStorageResource,

		Codec:          codec,
		StorageVersion: codecConfig.StorageVersion,
		MemoryVersion:  memoryVersion,
	}, nil
}

var legacyResourcesWithDefaultTableConvertor = map[schema.GroupResource]struct{}{
	apicore.Resource("limitranges"):              {},
	rbac.Resource("roles"):                       {},
	rbac.Resource("clusterroles"):                {},
	apisstorage.Resource("csistoragecapacities"): {},
}

func GetTableConvertor(gr schema.GroupResource) rest.TableConvertor {
	if _, ok := legacyResourcesWithDefaultTableConvertor[gr]; ok {
		return printers.NewDefaultTableConvertor(gr)
	}

	return printerstorage.TableConvertor{TableGenerator: printers.NewClusterTableGenerator().With(printersinternal.AddHandlers)}
}
