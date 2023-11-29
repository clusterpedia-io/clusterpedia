package apiserver

import (
	"context"
	"fmt"
	"net/http"

	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/discovery"
	clientrest "k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/api/clusterpedia/install"
	"github.com/clusterpedia-io/clusterpedia/pkg/apiserver/registry/clusterpedia/collectionresources"
	"github.com/clusterpedia-io/clusterpedia/pkg/apiserver/registry/clusterpedia/resources"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	informers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/filters"
)

var (
	// Scheme defines methods for serializing and deserializing API objects.
	Scheme = runtime.NewScheme()
	// Codecs provides methods for retrieving codecs and serializers for specific
	// versions and content types.
	Codecs = serializer.NewCodecFactory(Scheme)

	// ParameterCodec handles versioning of objects that are converted to query parameters.
	ParameterCodec = runtime.NewParameterCodec(Scheme)
)

func init() {
	install.Install(Scheme)

	// we need to add the options to empty v1
	// TODO fix the server code to avoid this
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Version: "v1"})
	_ = metainternal.AddToScheme(Scheme)

	// TODO: keep the generic API server from wanting this
	unversioned := schema.GroupVersion{Group: "", Version: "v1"}
	Scheme.AddUnversionedTypes(unversioned,
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
	)
}

// Config defines the config for the apiserver
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig

	StorageFactory storage.StorageFactory
}

type ClusterPediaServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig

	ClientConfig   *clientrest.Config
	StorageFactory storage.StorageFactory
}

// CompletedConfig embeds a private pointer that cannot be instantiated outside of this package.
type CompletedConfig struct {
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete() CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(),
		cfg.GenericConfig.ClientConfig,
		cfg.StorageFactory,
	}

	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}

	return CompletedConfig{&c}
}

func (config completedConfig) New() (*ClusterPediaServer, error) {
	if config.ClientConfig == nil {
		return nil, fmt.Errorf("CompletedConfig.New() called with config.ClientConfig == nil")
	}
	if config.StorageFactory == nil {
		return nil, fmt.Errorf("CompletedConfig.New() called with config.StorageFactory == nil")
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config.ClientConfig)
	if err != nil {
		return nil, err
	}
	initialAPIGroupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, err
	}

	crdclient, err := versioned.NewForConfig(config.ClientConfig)
	if err != nil {
		return nil, err
	}
	clusterpediaInformerFactory := informers.NewSharedInformerFactory(crdclient, 0)

	resourceServerConfig := kubeapiserver.NewDefaultConfig()
	resourceServerConfig.GenericConfig.ExternalAddress = config.GenericConfig.ExternalAddress
	resourceServerConfig.GenericConfig.LoopbackClientConfig = config.GenericConfig.LoopbackClientConfig
	resourceServerConfig.GenericConfig.TracerProvider = config.GenericConfig.TracerProvider
	resourceServerConfig.ExtraConfig = kubeapiserver.ExtraConfig{
		InformerFactory:          clusterpediaInformerFactory,
		StorageFactory:           config.StorageFactory,
		InitialAPIGroupResources: initialAPIGroupResources,
	}
	kubeResourceAPIServer, err := resourceServerConfig.Complete().New(genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	handlerChainFunc := config.GenericConfig.BuildHandlerChainFunc
	config.GenericConfig.BuildHandlerChainFunc = func(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
		handler := handlerChainFunc(apiHandler, c)
		handler = filters.WithRequestQuery(handler)
		handler = filters.WithAcceptHeader(handler)
		return handler
	}

	genericServer, err := config.GenericConfig.New("clusterpedia", hooksDelegate{kubeResourceAPIServer})
	if err != nil {
		return nil, err
	}

	v1beta1storage := map[string]rest.Storage{}
	v1beta1storage["resources"] = resources.NewREST(kubeResourceAPIServer.Handler)
	v1beta1storage["collectionresources"] = collectionresources.NewREST(config.GenericConfig.Serializer, config.StorageFactory)

	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(internal.GroupName, Scheme, ParameterCodec, Codecs)
	apiGroupInfo.VersionedResourcesStorageMap["v1beta1"] = v1beta1storage
	if err := genericServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, err
	}

	genericServer.AddPostStartHookOrDie("start-clusterpedia-informers", func(context genericapiserver.PostStartHookContext) error {
		clusterpediaInformerFactory.Start(context.StopCh)
		clusterpediaInformerFactory.WaitForCacheSync(context.StopCh)

		return nil
	})

	return &ClusterPediaServer{
		GenericAPIServer: genericServer,
	}, nil
}

func (server *ClusterPediaServer) Run(ctx context.Context) error {
	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}

type hooksDelegate struct {
	genericapiserver.DelegationTarget
}

func (s hooksDelegate) UnprotectedHandler() http.Handler {
	return nil
}

func (s hooksDelegate) HealthzChecks() []healthz.HealthChecker {
	return []healthz.HealthChecker{}
}

func (s hooksDelegate) ListedPaths() []string {
	return []string{}
}

func (s hooksDelegate) NextDelegate() genericapiserver.DelegationTarget {
	return nil
}
