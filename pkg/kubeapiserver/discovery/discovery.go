package discovery

import (
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type APIGroupSource interface {
	GetAPIGroups() map[string]metav1.APIGroup
}

type ResourceDiscoveryAPI struct {
	Group    string
	Resource metav1.APIResource
	Versions sets.Set[schema.GroupVersion]
}

// DiscoveryManager manage the cluster's discovery api and handle requests for /api and /apis
type DiscoveryManager struct {
	// groupSource used to ensure that the API Groups of all clusters are consistent
	groupSource APIGroupSource

	serializer                       runtime.NegotiatedSerializer
	stripVersionNegotiatedSerializer stripVersionNegotiatedSerializer

	discoveryLock  sync.Mutex
	groupHandler   *clusterGroupDiscoveryHandler
	versionHandler *clusterVersionDiscoveryHandler

	// apigroups is an apiGroup list supported by all clusters
	// clusterGroups API Groups supported by each cluster
	apigroups        atomic.Value // type: []metav1.APIGroup
	clusterAPIGroups atomic.Value // type: map[string][]metav1.APIGroup

	delegate http.Handler
}

// NewDiscoveryManager return a new instance of DiscoveryManager
func NewDiscoveryManager(serializer runtime.NegotiatedSerializer, groupSource APIGroupSource, delegate http.Handler) *DiscoveryManager {
	stripVersionNegotiatedSerializer := stripVersionNegotiatedSerializer{serializer}

	manager := &DiscoveryManager{
		serializer:                       serializer,
		stripVersionNegotiatedSerializer: stripVersionNegotiatedSerializer,

		groupSource: groupSource,
		delegate:    delegate,

		groupHandler: &clusterGroupDiscoveryHandler{
			serializer:                       serializer,
			stripVersionNegotiatedSerializer: stripVersionNegotiatedSerializer,
			delegate:                         delegate,
		},
		versionHandler: &clusterVersionDiscoveryHandler{
			serializer:                       serializer,
			stripVersionNegotiatedSerializer: stripVersionNegotiatedSerializer,
			delegate:                         delegate,
		},
	}

	manager.apigroups.Store([]metav1.APIGroup{})
	manager.clusterAPIGroups.Store(make(map[string][]metav1.APIGroup))

	// init group handler
	manager.groupHandler.handlers.Store(make(map[string]*groupDiscoveryHandler))
	manager.groupHandler.rebuildGlobalDiscoveryAPI(map[string]metav1.APIGroup{})

	// init version handler
	manager.versionHandler.handlers.Store(make(map[string]*versionDiscoveryHandler))
	manager.versionHandler.rebuildGlobalDiscoveryAPI()

	return manager
}

func (m *DiscoveryManager) ResourceEnabled(cluster string, gvr schema.GroupVersionResource) bool {
	var handler *versionDiscoveryHandler
	if cluster == "" {
		handler = m.versionHandler.global.Load().(*versionDiscoveryHandler)
	} else {
		handlers := m.versionHandler.handlers.Load().(map[string]*versionDiscoveryHandler)
		handler = handlers[cluster]
	}
	return handler != nil && handler.gvrs.Has(gvr.String())
}

func (m *DiscoveryManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pathParts := splitPath(req.URL.Path)
	if len(pathParts) == 0 || len(pathParts) > 3 {
		m.delegate.ServeHTTP(w, req)
		return
	}

	prefix := pathParts[0]
	if prefix == "api" {
		m.handleLegacyAPI(pathParts, w, req)
		return
	}
	if prefix != "apis" {
		m.delegate.ServeHTTP(w, req)
		return
	}

	// match /apis
	if len(pathParts) == 1 {
		var apigroups []metav1.APIGroup
		if cluster := request.ClusterNameValue(req.Context()); cluster != "" {
			var ok bool
			clusterAPIGroups := m.clusterAPIGroups.Load().(map[string][]metav1.APIGroup)
			if apigroups, ok = clusterAPIGroups[cluster]; !ok {
				m.delegate.ServeHTTP(w, req)
				return
			}
		} else {
			apigroups = m.apigroups.Load().([]metav1.APIGroup)
		}

		responsewriters.WriteObjectNegotiated(m.serializer, negotiation.DefaultEndpointRestrictions, schema.GroupVersion{}, w, req, http.StatusOK, &metav1.APIGroupList{Groups: apigroups}, false)
		return
	}

	// match /apis/<group>
	if len(pathParts) == 2 {
		m.groupHandler.ServeHTTP(w, req)
		return
	}

	// match /apis/<group>/<version>
	m.versionHandler.ServeHTTP(w, req)
}

func (m *DiscoveryManager) handleLegacyAPI(pathParts []string, w http.ResponseWriter, req *http.Request) {
	if len(pathParts) > 2 {
		m.delegate.ServeHTTP(w, req)
		return
	}

	// match /api
	if len(pathParts) == 1 {
		apiVersions := &metav1.APIVersions{
			Versions: []string{"v1"},
		}

		responsewriters.WriteObjectNegotiated(m.stripVersionNegotiatedSerializer, negotiation.DefaultEndpointRestrictions, schema.GroupVersion{}, w, req, http.StatusOK, apiVersions, false)
		return
	}

	// match /api/v1
	m.versionHandler.ServeHTTP(w, req)
}

func (m *DiscoveryManager) SetClusterGroupResource(cluster string, apis map[schema.GroupResource]ResourceDiscoveryAPI) {
	apiversions := make(map[schema.GroupVersion][]metav1.APIResource)
	for _, api := range apis {
		for version := range api.Versions {
			apiversions[version] = append(apiversions[version], api.Resource)
		}
	}

	currentversions := m.versionHandler.getClusterDiscoveryAPI(cluster)
	if reflect.DeepEqual(currentversions, apiversions) {
		return
	}

	m.discoveryLock.Lock()
	defer m.discoveryLock.Unlock()

	// set cluster's resource versions
	m.versionHandler.setClusterDiscoveryAPI(cluster, apiversions)
	m.versionHandler.rebuildGlobalDiscoveryAPI()

	allgroups := m.groupSource.GetAPIGroups()
	apigroups := buildAPIGroups(sets.KeySet(apiversions), allgroups)

	currentgroups := m.groupHandler.getClusterDiscoveryAPI(cluster)
	if reflect.DeepEqual(apigroups, currentgroups) {
		return
	}

	m.groupHandler.setClusterDiscoveryAPI(cluster, apigroups)
	m.groupHandler.rebuildGlobalDiscoveryAPI(allgroups)

	m.rebuildClusterDiscoveryAPI(cluster)
	m.rebuildGlobalDiscoveryAPI()
}

func (m *DiscoveryManager) rebuildClusterDiscoveryAPI(cluster string) {
	clustergroups := make(map[string][]metav1.APIGroup)
	for cluster, gs := range m.clusterAPIGroups.Load().(map[string][]metav1.APIGroup) {
		clustergroups[cluster] = gs
	}

	currentgroups := m.groupHandler.getClusterDiscoveryAPI(cluster)
	if currentgroups == nil {
		delete(clustergroups, cluster)
		m.clusterAPIGroups.Store(clustergroups)
		return
	}

	apigroups := make([]metav1.APIGroup, 0, len(currentgroups))
	for name, group := range currentgroups {
		if name != "" {
			apigroups = append(apigroups, group)
		}
	}
	sortAPIGroupByName(apigroups)

	clustergroups[cluster] = apigroups
	m.clusterAPIGroups.Store(clustergroups)
}

func (m *DiscoveryManager) rebuildGlobalDiscoveryAPI() {
	currentgroups := m.groupHandler.getGlobalDiscoveryAPI()

	apigroups := make([]metav1.APIGroup, 0, len(currentgroups))
	for name, group := range currentgroups {
		if name != "" {
			apigroups = append(apigroups, group)
		}
	}
	sortAPIGroupByName(apigroups)

	m.apigroups.Store(apigroups)
}

func (m *DiscoveryManager) RemoveCluster(cluster string) {
	clustergroups := m.clusterAPIGroups.Load().(map[string][]metav1.APIGroup)
	if _, ok := clustergroups[cluster]; !ok {
		return
	}

	m.discoveryLock.Lock()
	defer m.discoveryLock.Unlock()

	if m.versionHandler.getClusterDiscoveryAPI(cluster) != nil {
		m.versionHandler.removeClusterDiscoveryAPI(cluster)
		m.versionHandler.rebuildGlobalDiscoveryAPI()
	}

	if m.groupHandler.getClusterDiscoveryAPI(cluster) != nil {
		m.groupHandler.removeClusterDiscoveryAPI(cluster)
		m.groupHandler.rebuildGlobalDiscoveryAPI(m.groupSource.GetAPIGroups())

		m.rebuildClusterDiscoveryAPI(cluster)
		m.rebuildGlobalDiscoveryAPI()
	}
}
