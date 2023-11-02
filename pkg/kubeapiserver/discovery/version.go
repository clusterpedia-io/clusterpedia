package discovery

import (
	"net/http"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type versionDiscoveryHandler struct {
	serializer                       runtime.NegotiatedSerializer
	stripVersionNegotiatedSerializer stripVersionNegotiatedSerializer

	gvrs      sets.Set[string]
	resources map[schema.GroupVersion][]metav1.APIResource
	delegate  http.Handler
}

func (h *versionDiscoveryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pathParts := splitPath(req.URL.Path)
	if len(pathParts) < 2 {
		h.delegate.ServeHTTP(w, req)
		return
	}

	var groupVersion schema.GroupVersion
	switch pathParts[0] {
	case "api":
		if len(pathParts) == 2 {
			groupVersion.Version = pathParts[1]
		}
	case "apis":
		if len(pathParts) == 3 {
			groupVersion.Group, groupVersion.Version = pathParts[1], pathParts[2]
		}
	}

	if groupVersion.Empty() {
		h.delegate.ServeHTTP(w, req)
		return
	}

	resources, ok := h.resources[groupVersion]
	if !ok {
		h.delegate.ServeHTTP(w, req)
		return
	}

	serializer := h.serializer
	if keepUnversioned(groupVersion.Group) {
		serializer = h.stripVersionNegotiatedSerializer
	}

	responsewriters.WriteObjectNegotiated(
		serializer, negotiation.DefaultEndpointRestrictions, schema.GroupVersion{}, w, req, http.StatusOK,
		&metav1.APIResourceList{GroupVersion: groupVersion.String(), APIResources: resources}, false,
	)
}

type clusterVersionDiscoveryHandler struct {
	serializer                       runtime.NegotiatedSerializer
	stripVersionNegotiatedSerializer stripVersionNegotiatedSerializer

	handlers atomic.Value // map[string]*versionDiscoveryHandler
	global   atomic.Value // *versionDiscoveryHandler

	delegate http.Handler
}

func (h *clusterVersionDiscoveryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	cluster := request.ClusterNameValue(req.Context())
	if cluster == "" {
		h.global.Load().(*versionDiscoveryHandler).ServeHTTP(w, req)
		return
	}

	handlers := h.handlers.Load().(map[string]*versionDiscoveryHandler)
	if handler, ok := handlers[cluster]; !ok {
		h.delegate.ServeHTTP(w, req)
	} else {
		handler.ServeHTTP(w, req)
	}
}

func (h *clusterVersionDiscoveryHandler) getClusterDiscoveryAPI(cluster string) map[schema.GroupVersion][]metav1.APIResource {
	handlers := h.handlers.Load().(map[string]*versionDiscoveryHandler)
	handler, ok := handlers[cluster]
	if !ok {
		return nil
	}

	return handler.resources
}

func (h *clusterVersionDiscoveryHandler) setClusterDiscoveryAPI(cluster string, resources map[schema.GroupVersion][]metav1.APIResource) {
	handlers := make(map[string]*versionDiscoveryHandler)
	for cluster, handler := range h.handlers.Load().(map[string]*versionDiscoveryHandler) {
		handlers[cluster] = handler
	}

	gvrs := sets.Set[string]{}
	for gv, rs := range resources {
		for _, r := range rs {
			gvrs.Insert(gv.WithResource(r.Name).String())
		}
	}

	handlers[cluster] = &versionDiscoveryHandler{
		serializer:                       h.serializer,
		stripVersionNegotiatedSerializer: h.stripVersionNegotiatedSerializer,
		resources:                        resources,
		gvrs:                             gvrs,
		delegate:                         h.delegate,
	}
	h.handlers.Store(handlers)
}

func (h *clusterVersionDiscoveryHandler) removeClusterDiscoveryAPI(cluster string) {
	currentHandlers := h.handlers.Load().(map[string]*versionDiscoveryHandler)
	if _, ok := currentHandlers[cluster]; !ok {
		return
	}

	handlers := make(map[string]*versionDiscoveryHandler, len(currentHandlers)-1)
	for name, handler := range currentHandlers {
		if name != cluster {
			handlers[name] = handler
		}
	}
	h.handlers.Store(handlers)
}

func (h *clusterVersionDiscoveryHandler) rebuildGlobalDiscoveryAPI() {
	// summarize the resources of all clusters
	apiresources := make(map[schema.GroupVersionResource]metav1.APIResource)
	for _, handler := range h.handlers.Load().(map[string]*versionDiscoveryHandler) {
		for gv, resources := range handler.resources {
			for _, resource := range resources {
				apiresources[gv.WithResource(resource.Name)] = resource
			}
		}
	}

	gvrs := sets.Set[string]{}
	versionResources := make(map[schema.GroupVersion][]metav1.APIResource)
	for gvr, apiResource := range apiresources {
		gvrs.Insert(gvr.String())

		gv := gvr.GroupVersion()
		versionResources[gv] = append(versionResources[gv], apiResource)
	}

	h.global.Store(&versionDiscoveryHandler{
		serializer:                       h.serializer,
		stripVersionNegotiatedSerializer: h.stripVersionNegotiatedSerializer,
		resources:                        versionResources,
		gvrs:                             gvrs,
		delegate:                         h.delegate,
	})
}
