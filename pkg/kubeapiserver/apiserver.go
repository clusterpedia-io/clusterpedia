package kubeapiserver

import (
	"errors"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfeatures "k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/healthz"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/restmapper"
	"k8s.io/component-base/tracing"

	informers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/filters"
	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

var (
	Scheme = runtime.NewScheme()
	Codecs = serializer.NewCodecFactory(Scheme)
)

func init() {
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Group: "", Version: "v1"})
	Scheme.AddUnversionedTypes(schema.GroupVersion{Group: "", Version: "v1"},
		&metav1.Status{},
		&metav1.APIVersions{},
		&metav1.APIGroupList{},
		&metav1.APIGroup{},
		&metav1.APIResourceList{},
		&metav1.WatchEvent{},
	)
}

func NewDefaultConfig() *Config {
	genericConfig := genericapiserver.NewRecommendedConfig(Codecs)

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
	c.GenericConfig.RequestInfoResolver = wrapRequestInfoResolverForNamespace{
		c.GenericConfig.RequestInfoResolver,
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

	restManager := NewRESTManager(c.GenericConfig.Serializer, runtime.ContentTypeJSON, c.ExtraConfig.StorageFactory, c.ExtraConfig.InitialAPIGroupResources)
	discoveryManager := discovery.NewDiscoveryManager(c.GenericConfig.Serializer, restManager, delegate)

	// handle root discovery request
	genericserver.Handler.NonGoRestfulMux.Handle("/api", discoveryManager)
	genericserver.Handler.NonGoRestfulMux.Handle("/apis", discoveryManager)

	resourceHandler := &ResourceHandler{
		minRequestTimeout: time.Duration(c.GenericConfig.MinRequestTimeout) * time.Second,

		delegate:      delegate,
		rest:          restManager,
		discovery:     discoveryManager,
		clusterLister: c.ExtraConfig.InformerFactory.Cluster().V1alpha2().PediaClusters().Lister(),
	}
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/api/", resourceHandler)
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/apis/", resourceHandler)

	_ = NewClusterResourceController(restManager, discoveryManager, c.ExtraConfig.InformerFactory.Cluster().V1alpha2().PediaClusters())
	return genericserver, nil
}

func BuildHandlerChain(apiHandler http.Handler, c *genericapiserver.Config) http.Handler {
	handler := genericapifilters.WithRequestInfo(apiHandler, c.RequestInfoResolver)
	handler = genericfilters.WithPanicRecovery(handler, c.RequestInfoResolver)

	// https://github.com/clusterpedia-io/clusterpedia/issues/54
	handler = filters.RemoveFieldSelectorFromRequest(handler)

	if utilfeature.DefaultFeatureGate.Enabled(genericfeatures.APIServerTracing) {
		handler = tracing.WithTracing(handler, c.TracerProvider, "ClusterpediaAPI")
	}

	/* used for debugging
	handler = genericapifilters.WithWarningRecorder(handler)
	handler = WithClusterName(handler, "cluster-1")
	*/
	return handler
}

/* used for debugging
func WithClusterName(handler http.Handler, cluster string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(request.WithClusterName(req.Context(), cluster))
		handler.ServeHTTP(w, req)
	})
}
*/

type wrapRequestInfoResolverForNamespace struct {
	genericrequest.RequestInfoResolver
}

func (r wrapRequestInfoResolverForNamespace) NewRequestInfo(req *http.Request) (*genericrequest.RequestInfo, error) {
	info, err := r.RequestInfoResolver.NewRequestInfo(req)
	if err != nil {
		return nil, err
	}

	if info.Resource == "namespaces" {
		info.Namespace = ""
	}
	return info, nil
}
