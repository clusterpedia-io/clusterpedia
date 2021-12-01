package kubeapiserver

import (
	"errors"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/restmapper"

	informers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/legacyresource"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

func NewDefaultConfig() *Config {
	genericConfig := genericapiserver.NewRecommendedConfig(legacyresource.Codecs)

	genericConfig.APIServerID = ""
	genericConfig.EnableIndex = false
	genericConfig.EnableDiscovery = false
	genericConfig.EnableProfiling = false
	genericConfig.EnableMetrics = false
	genericConfig.BuildHandlerChainFunc = BuildHandlerChain
	genericConfig.HealthzChecks = []healthz.HealthChecker{healthz.PingHealthz}
	genericConfig.ReadyzChecks = []healthz.HealthChecker{healthz.PingHealthz}
	genericConfig.LivezChecks = []healthz.HealthChecker{healthz.PingHealthz}

	// disable genericapiserver's default post start hooks
	const maxInFlightFilterHookName = "max-in-flight-filter"
	genericConfig.DisabledPostStartHooks.Insert(maxInFlightFilterHookName)

	return &Config{GenericConfig: genericConfig}
}

type ExtraConfig struct {
	StorageFactory           storage.StorageFactory
	InformerFactory          informers.SharedInformerFactory
	InitialAPIGroupResources []*restmapper.APIGroupResources
}

type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig

	ExtraConfig ExtraConfig
}

func (c *Config) Complete() CompletedConfig {
	completed := &completedConfig{
		GenericConfig: c.GenericConfig.Complete(),
		ExtraConfig:   &c.ExtraConfig,
	}

	if c.GenericConfig.Version == nil {
		version := version.GetKubeVersion()
		c.GenericConfig.Version = &version
	}
	return CompletedConfig{completed}
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig

	ExtraConfig *ExtraConfig
}

type CompletedConfig struct {
	*completedConfig
}

func (c completedConfig) New(delegationTarget genericapiserver.DelegationTarget) (*genericapiserver.GenericAPIServer, error) {
	if c.ExtraConfig.StorageFactory == nil {
		return nil, errors.New("kubeapiserver.New() called with config.StorageFactory == nil")
	}
	if c.ExtraConfig.InformerFactory == nil {
		return nil, errors.New("kubeapiserver.New() called with config.InformerFactory == nil")
	}

	genericserver, err := c.GenericConfig.New("clusterpedia-kube-apiserver", delegationTarget)
	if err != nil {
		return nil, err
	}

	delegate := delegationTarget.UnprotectedHandler()
	if delegate == nil {
		delegate = http.NotFoundHandler()
	}

	restManager := NewRESTManager(runtime.ContentTypeJSON, c.ExtraConfig.StorageFactory, c.ExtraConfig.InitialAPIGroupResources)
	discoveryManager := discovery.NewDiscoveryManager(c.GenericConfig.Serializer, restManager, delegate)

	// handle root discovery request
	genericserver.Handler.NonGoRestfulMux.Handle("/api", discoveryManager)
	genericserver.Handler.NonGoRestfulMux.Handle("/apis", discoveryManager)

	resourceHandler := &ResourceHandler{
		minRequestTimeout: c.GenericConfig.RequestTimeout,

		delegate:  delegate,
		rest:      restManager,
		discovery: discoveryManager,
	}
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/api/", resourceHandler)
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/apis/", resourceHandler)

	_ = NewClusterResourceController(restManager, discoveryManager, c.ExtraConfig.InformerFactory.Clusters().V1alpha1().PediaClusters())
	return genericserver, nil
}

func BuildHandlerChain(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
	handler := genericapifilters.WithRequestInfo(apiHandler, c.RequestInfoResolver)
	handler = genericfilters.WithPanicRecovery(handler, c.RequestInfoResolver)

	//	handler = filters.WithRequestQuery(handler)
	return handler
}
