package kubeapiserver

import (
	"errors"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/sets"
	genericapifilters "k8s.io/apiserver/pkg/endpoints/filters"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
	genericfeatures "k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericfilters "k8s.io/apiserver/pkg/server/filters"
	"k8s.io/apiserver/pkg/server/healthz"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/apiserver/pkg/util/version"
	"k8s.io/client-go/restmapper"
	"k8s.io/component-base/tracing"

	informers "github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	proxyrest "github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcerest/proxy"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/filters"
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
	AllowPediaClusterConfigReuse    bool
	ExtraProxyRequestHeaderPrefixes []string
	AllowedProxySubresources        map[schema.GroupResource]sets.Set[string]
}

type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig

	StorageFactory           storage.StorageFactory
	InformerFactory          informers.SharedInformerFactory
	InitialAPIGroupResources []*restmapper.APIGroupResources

	ExtraConfig *ExtraConfig
}

func (c *Config) Complete() CompletedConfig {
	if c.GenericConfig.EffectiveVersion == nil {
		c.GenericConfig.EffectiveVersion = version.DefaultKubeEffectiveVersion()
	}

	completed := &completedConfig{
		GenericConfig:            c.GenericConfig.Complete(),
		StorageFactory:           c.StorageFactory,
		InformerFactory:          c.InformerFactory,
		InitialAPIGroupResources: c.InitialAPIGroupResources,
		ExtraConfig:              c.ExtraConfig,
	}

	c.GenericConfig.RequestInfoResolver = wrapRequestInfoResolverForNamespace{
		c.GenericConfig.RequestInfoResolver,
	}
	return CompletedConfig{completed}
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig

	StorageFactory           storage.StorageFactory
	InformerFactory          informers.SharedInformerFactory
	InitialAPIGroupResources []*restmapper.APIGroupResources
	ExtraConfig              *ExtraConfig
}

type CompletedConfig struct {
	*completedConfig
}

var sortedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

func (c completedConfig) New(delegationTarget genericapiserver.DelegationTarget) (*genericapiserver.GenericAPIServer, []string, error) {
	if c.StorageFactory == nil {
		return nil, nil, errors.New("kubeapiserver.New() called with config.StorageFactory == nil")
	}
	if c.InformerFactory == nil {
		return nil, nil, errors.New("kubeapiserver.New() called with config.InformerFactory == nil")
	}

	genericserver, err := c.GenericConfig.New("clusterpedia-kube-apiserver", delegationTarget)
	if err != nil {
		return nil, nil, err
	}

	delegate := delegationTarget.UnprotectedHandler()
	if delegate == nil {
		delegate = http.NotFoundHandler()
	}

	restManager := NewRESTManager(c.GenericConfig.Serializer, runtime.ContentTypeJSON, c.StorageFactory, c.InitialAPIGroupResources)
	discoveryManager := discovery.NewDiscoveryManager(c.GenericConfig.Serializer, restManager, delegate)

	// handle root discovery request
	genericserver.Handler.NonGoRestfulMux.Handle("/api", discoveryManager)
	genericserver.Handler.NonGoRestfulMux.Handle("/apis", discoveryManager)

	resourceHandler := &ResourceHandler{
		minRequestTimeout: time.Duration(c.GenericConfig.MinRequestTimeout) * time.Second,

		delegate:      delegate,
		rest:          restManager,
		discovery:     discoveryManager,
		clusterLister: c.InformerFactory.Cluster().V1alpha2().PediaClusters().Lister(),
	}
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/api/", resourceHandler)
	genericserver.Handler.NonGoRestfulMux.HandlePrefix("/apis/", resourceHandler)

	clusterInformer := c.InformerFactory.Cluster().V1alpha2().PediaClusters()
	_ = NewClusterResourceController(restManager, discoveryManager, clusterInformer)

	connector := proxyrest.NewProxyConnector(clusterInformer.Lister(), c.ExtraConfig.AllowPediaClusterConfigReuse, c.ExtraConfig.ExtraProxyRequestHeaderPrefixes)

	methodSet := sets.New("GET")
	for _, rest := range proxyrest.GetSubresourceRESTs(connector) {
		allows := c.ExtraConfig.AllowedProxySubresources[rest.ParentGroupResource()]
		if allows == nil || !allows.Has(rest.Subresource()) {
			continue
		}
		if err := restManager.preRegisterSubresource(subresource{
			gr:         rest.ParentGroupResource(),
			kind:       rest.ParentKind(),
			namespaced: rest.Namespaced(),

			name:      rest.Subresource(),
			connecter: rest,
		}); err != nil {
			return nil, nil, err
		}
		methodSet.Insert(rest.ConnectMethods()...)
	}

	var methods []string
	for _, m := range sortedMethods {
		if methodSet.Has(m) {
			methods = append(methods, m)
		}
	}

	resourceHandler.proxy = proxyrest.NewRemoteProxyREST(c.GenericConfig.Serializer, connector)
	return genericserver, methods, nil
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
