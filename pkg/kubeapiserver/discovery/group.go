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

type groupDiscoveryHandler struct {
	serializer                       runtime.NegotiatedSerializer
	stripVersionNegotiatedSerializer stripVersionNegotiatedSerializer

	groups   map[string]metav1.APIGroup
	delegate http.Handler
}

func (h *groupDiscoveryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	pathParts := splitPath(req.URL.Path)
	if len(pathParts) != 2 || pathParts[0] != "apis" {
		h.delegate.ServeHTTP(w, req)
		return
	}

	group, ok := h.groups[pathParts[1]]
	if !ok {
		h.delegate.ServeHTTP(w, req)
		return
	}

	serializer := h.serializer
	if keepUnversioned(pathParts[1]) {
		serializer = h.stripVersionNegotiatedSerializer
	}
	responsewriters.WriteObjectNegotiated(serializer, negotiation.DefaultEndpointRestrictions, schema.GroupVersion{}, w, req, http.StatusOK, &group, false)
}

type clusterGroupDiscoveryHandler struct {
	serializer                       runtime.NegotiatedSerializer
	stripVersionNegotiatedSerializer stripVersionNegotiatedSerializer

	global   atomic.Value // *groupDiscoveryHandler
	handlers atomic.Value // map[string]*groupDiscoveryHandler

	delegate http.Handler
}

func (h *clusterGroupDiscoveryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	cluster := request.ClusterNameValue(req.Context())
	if cluster == "" {
		h.global.Load().(*groupDiscoveryHandler).ServeHTTP(w, req)
		return
	}

	handlers := h.handlers.Load().(map[string]*groupDiscoveryHandler)
	handler, ok := handlers[cluster]
	if !ok {
		h.delegate.ServeHTTP(w, req)
		return
	}

	handler.ServeHTTP(w, req)
}

func (h *clusterGroupDiscoveryHandler) setClusterDiscoveryAPI(cluster string, groups map[string]metav1.APIGroup) {
	handlers := make(map[string]*groupDiscoveryHandler)
	for cluster, handler := range h.handlers.Load().(map[string]*groupDiscoveryHandler) {
		handlers[cluster] = handler
	}

	handlers[cluster] = &groupDiscoveryHandler{
		serializer:                       h.serializer,
		stripVersionNegotiatedSerializer: h.stripVersionNegotiatedSerializer,
		groups:                           groups,
		delegate:                         h.delegate,
	}
	h.handlers.Store(handlers)
}

func (h *clusterGroupDiscoveryHandler) getClusterDiscoveryAPI(cluster string) map[string]metav1.APIGroup {
	handlers := h.handlers.Load().(map[string]*groupDiscoveryHandler)
	handler, ok := handlers[cluster]
	if !ok {
		return nil
	}
	return handler.groups
}

func (h *clusterGroupDiscoveryHandler) getGlobalDiscoveryAPI() map[string]metav1.APIGroup {
	return h.global.Load().(*groupDiscoveryHandler).groups
}

func (h *clusterGroupDiscoveryHandler) removeClusterDiscoveryAPI(cluster string) {
	currentHandlers := h.handlers.Load().(map[string]*groupDiscoveryHandler)
	if _, ok := currentHandlers[cluster]; !ok {
		return
	}

	handlers := make(map[string]*groupDiscoveryHandler, len(currentHandlers)-1)
	for name, handler := range currentHandlers {
		if name != cluster {
			handlers[name] = handler
		}
	}
	h.handlers.Store(handlers)
}

func (h *clusterGroupDiscoveryHandler) rebuildGlobalDiscoveryAPI(source map[string]metav1.APIGroup) {
	groupversions := sets.Set[schema.GroupVersion]{}
	for _, handler := range h.handlers.Load().(map[string]*groupDiscoveryHandler) {
		for group, apiGroup := range handler.groups {
			for _, version := range apiGroup.Versions {
				groupversions.Insert(schema.GroupVersion{Group: group, Version: version.Version})
			}
		}
	}

	apiGroups := buildAPIGroups(groupversions, source)
	h.global.Store(&groupDiscoveryHandler{
		serializer:                       h.serializer,
		stripVersionNegotiatedSerializer: h.stripVersionNegotiatedSerializer,
		groups:                           apiGroups,
		delegate:                         h.delegate,
	})
}

func buildAPIGroups(groupversions sets.Set[schema.GroupVersion], source map[string]metav1.APIGroup) map[string]metav1.APIGroup {
	groups := sets.Set[string]{}
	unsortedVersions := make(map[string][]metav1.GroupVersionForDiscovery)
	for gv := range groupversions {
		groups.Insert(gv.Group)

		if apiGroup := source[gv.Group]; len(apiGroup.Versions) == 0 {
			unsortedVersions[gv.Group] = append(unsortedVersions[gv.Group],
				metav1.GroupVersionForDiscovery{
					GroupVersion: gv.String(),
					Version:      gv.Version,
				},
			)
		}
	}

	apiGroups := make(map[string]metav1.APIGroup, len(groups))
	for group := range groups {
		apiGroup, ok := source[group]
		if !ok {
			continue
		}

		vds, ok := unsortedVersions[group]
		if ok {
			sortGroupDiscoveryByKubeAwareVersion(vds)
		} else {
			for _, vd := range apiGroup.Versions {
				if _, ok := groupversions[schema.GroupVersion{Group: apiGroup.Name, Version: vd.Version}]; ok {
					vds = append(vds, vd)
				}
			}
		}

		apiGroup.Versions = vds
		if len(vds) != 0 {
			apiGroup.PreferredVersion = vds[0]
		}

		apiGroups[apiGroup.Name] = apiGroup
	}
	return apiGroups
}
