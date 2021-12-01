package kubeapiserver

import (
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/legacyresource"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcerest"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type RESTManager struct {
	storageFactory              storage.StorageFactory
	legacyResourcetSorageConfig *legacyresource.StorageConfigFactory
	equivalentResourceRegistry  runtime.EquivalentResourceMapper

	lock      sync.Mutex
	groups    atomic.Value // map[string]metav1.APIGroup
	resources atomic.Value // map[schema.GroupResource]metav1.APIResource

	restResourceInfos atomic.Value // map[schema.GroupVersionResource]RESTResourceInfo
}

func NewRESTManager(storageMediaType string, storageFactory storage.StorageFactory, initialAPIGroupResources []*restmapper.APIGroupResources) *RESTManager {
	apiresources := make(map[schema.GroupResource]metav1.APIResource)
	for _, groupresources := range initialAPIGroupResources {
		group := groupresources.Group
		for _, version := range group.Versions {
			resources, ok := groupresources.VersionedResources[version.Version]
			if !ok {
				continue
			}

			for _, resource := range resources {
				if strings.Contains(resource.Name, "/") {
					// skip subresources
					continue
				}

				gr := schema.GroupResource{Group: group.Name, Resource: resource.Name}
				if _, ok := apiresources[gr]; ok {
					continue
				}

				gk := schema.GroupKind{Group: group.Name, Kind: resource.Kind}
				if gvs := legacyresource.Scheme.VersionsForGroupKind(gk); len(gvs) == 0 {
					// skip custom resource
					continue
				}

				// clusterpedia's kube resource only support get and list
				resource.Verbs = metav1.Verbs{"get", "list"}
				apiresources[gr] = resource
			}
		}
	}

	manager := &RESTManager{
		storageFactory:              storageFactory,
		legacyResourcetSorageConfig: legacyresource.NewStorageConfigFactory(storageMediaType),
		equivalentResourceRegistry:  runtime.NewEquivalentResourceRegistry(),
	}

	manager.resources.Store(apiresources)
	manager.groups.Store(map[string]metav1.APIGroup{})
	manager.restResourceInfos.Store(make(map[schema.GroupVersionResource]RESTResourceInfo))
	return manager
}

func (m *RESTManager) GetAPIGroups() map[string]metav1.APIGroup {
	return m.groups.Load().(map[string]metav1.APIGroup)
}

func (m *RESTManager) GetRESTResourceInfo(gvr schema.GroupVersionResource) RESTResourceInfo {
	infos := m.restResourceInfos.Load().(map[schema.GroupVersionResource]RESTResourceInfo)
	return infos[gvr]
}

func (m *RESTManager) LoadResources(infos ResourceInfoMap) map[schema.GroupResource]discovery.ResourceDiscoveryAPI {
	apigroups := m.groups.Load().(map[string]metav1.APIGroup)
	apiresources := m.resources.Load().(map[schema.GroupResource]metav1.APIResource)
	restinfos := m.restResourceInfos.Load().(map[schema.GroupVersionResource]RESTResourceInfo)

	addedAPIGroups := make(map[string]metav1.APIGroup)
	addedAPIResources := make(map[schema.GroupResource]metav1.APIResource)
	addedInfos := make(map[schema.GroupVersionResource]RESTResourceInfo)

	apis := make(map[schema.GroupResource]discovery.ResourceDiscoveryAPI)
	for gr, info := range infos {
		if _, hasGroup := apigroups[gr.Group]; !hasGroup {
			if _, hasGroup := addedAPIGroups[gr.Group]; !hasGroup {
				group := metav1.APIGroup{
					Name: gr.Group,
				}

				// for custom resources, the prioritizedVersions is empty
				for _, gv := range legacyresource.Scheme.PrioritizedVersionsForGroup(gr.Group) {
					group.Versions = append(group.Versions, metav1.GroupVersionForDiscovery{
						GroupVersion: gv.String(),
						Version:      gv.Version,
					})
				}
				addedAPIGroups[group.Name] = group
			}
		}

		gk := schema.GroupKind{Group: gr.Group, Kind: info.Kind}
		versions := legacyresource.Scheme.VersionsForGroupKind(gk)
		if len(versions) == 0 {
			// not support custom resource
			// versions = []schema.GroupVersion{schema.GroupVersion{info.Group, info.Version}}
			continue
		}

		resource, hasResource := apiresources[gr]
		if !hasResource {
			resource = metav1.APIResource{Name: gr.Resource, Namespaced: info.Namespaced, Kind: info.Kind}
			addedAPIResources[gr] = resource
		}

		api := discovery.ResourceDiscoveryAPI{
			Group:    gr.Group,
			Resource: resource,
			Versions: make(map[schema.GroupVersion]struct{}, len(versions)),
		}
		for _, version := range versions {
			api.Versions[version] = struct{}{}

			gvr := gr.WithVersion(version.Version)
			if _, ok := restinfos[gvr]; !ok {
				addedInfos[gvr] = RESTResourceInfo{APIResource: resource}
			}
		}
		apis[gr] = api

	}

	//  no new APIGroups or APIResources or RESTResourceInfo
	if len(addedAPIGroups) == 0 && len(addedAPIResources) == 0 && len(addedInfos) == 0 {
		return apis
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	if len(addedAPIGroups) != 0 {
		m.addAPIGroupsLocked(addedAPIGroups)
	}
	if len(addedAPIResources) != 0 {
		m.addAPIResourcesLocked(addedAPIResources)
	}
	if len(addedInfos) != 0 {
		m.addRESTResourceInfosLocked(addedInfos)
	}
	return apis
}

func (m *RESTManager) addAPIGroupsLocked(addedGroups map[string]metav1.APIGroup) {
	apigroups := m.groups.Load().(map[string]metav1.APIGroup)

	groups := make(map[string]metav1.APIGroup, len(apigroups)+len(addedGroups))
	for g, apigroup := range apigroups {
		groups[g] = apigroup
	}
	for g, apigroup := range addedGroups {
		groups[g] = apigroup
	}

	m.groups.Store(groups)
}

func (m *RESTManager) addAPIResourcesLocked(addedResources map[schema.GroupResource]metav1.APIResource) {
	apiresources := m.resources.Load().(map[schema.GroupResource]metav1.APIResource)

	resources := make(map[schema.GroupResource]metav1.APIResource, len(apiresources)+len(addedResources))
	for gr, apiresource := range apiresources {
		resources[gr] = apiresource
	}
	for gr, apiresource := range addedResources {
		resources[gr] = apiresource
	}

	m.resources.Store(resources)
}

func (m *RESTManager) addRESTResourceInfosLocked(addedInfos map[schema.GroupVersionResource]RESTResourceInfo) {
	restinfos := m.restResourceInfos.Load().(map[schema.GroupVersionResource]RESTResourceInfo)

	infos := make(map[schema.GroupVersionResource]RESTResourceInfo, len(restinfos)+len(addedInfos))
	for gvr, info := range restinfos {
		infos[gvr] = info
	}

	for gvr, info := range addedInfos {
		if _, ok := infos[gvr]; ok {
			continue
		}

		if info.Storage == nil {
			storage, err := m.genLegacyResourceRESTStorage(gvr, info.APIResource.Kind)
			if err != nil {
				klog.ErrorS(err, "Failed to gen resource rest storage", "gvr", gvr, "kind", info.APIResource.Kind)
				continue
			}

			info.Storage = storage
		}

		if info.RequestScope == nil {
			requestScope := m.genLegacyResourceRequestScope(gvr, info.APIResource.Kind, info.APIResource.Namespaced)
			requestScope.TableConvertor = info.Storage

			info.RequestScope = requestScope
		}

		infos[gvr] = info
	}

	m.restResourceInfos.Store(infos)
}

func (m *RESTManager) genLegacyResourceRESTStorage(gvr schema.GroupVersionResource, kind string) (*resourcerest.RESTStorage, error) {
	storageConfig, err := m.legacyResourcetSorageConfig.NewConfig(gvr)
	if err != nil {
		return nil, err
	}

	resourceStorage, err := m.storageFactory.NewResourceStorage(storageConfig)
	if err != nil {
		return nil, err
	}

	return &resourcerest.RESTStorage{
		DefaultQualifiedResource: gvr.GroupResource(),

		NewFunc: func() runtime.Object {
			obj, _ := legacyresource.Scheme.New(schema.GroupVersionKind{Group: gvr.Group, Version: runtime.APIVersionInternal, Kind: kind})
			return obj
		},
		NewListFunc: func() runtime.Object {
			obj, _ := legacyresource.Scheme.New(schema.GroupVersionKind{Group: gvr.Group, Version: runtime.APIVersionInternal, Kind: kind + "List"})
			return obj
		},

		Storage:        resourceStorage,
		TableConvertor: legacyresource.GetTableConvertor(gvr.GroupResource()),
	}, nil
}

// TODO: support custom resource
func (m *RESTManager) genLegacyResourceRequestScope(gvr schema.GroupVersionResource, kind string, namespaced bool) *handlers.RequestScope {
	namer := handlers.ContextBasedNaming{
		SelfLinker:    runtime.SelfLinker(meta.NewAccessor()),
		ClusterScoped: !namespaced,
	}
	if gvr.Group == "" {
		namer.SelfLinkPathPrefix = path.Join("api", gvr.Version) + "/"
	} else {
		namer.SelfLinkPathPrefix = path.Join("apis", gvr.Group, gvr.Version) + "/"
	}

	return &handlers.RequestScope{
		Namer:          namer,
		Serializer:     legacyresource.Codecs,
		ParameterCodec: legacyresource.ParameterCodec,
		Creater:        legacyresource.Scheme,
		Convertor:      legacyresource.Scheme,
		Defaulter:      legacyresource.Scheme,
		Typer:          legacyresource.Scheme,

		Resource:         gvr,
		Kind:             gvr.GroupVersion().WithKind(kind),
		MetaGroupVersion: metav1.SchemeGroupVersion,

		EquivalentResourceMapper: m.equivalentResourceRegistry,
	}
}

type RESTResourceInfo struct {
	APIResource  metav1.APIResource
	RequestScope *handlers.RequestScope
	Storage      *resourcerest.RESTStorage
}

func (info RESTResourceInfo) Empty() bool {
	return info.APIResource.Name == "" || info.RequestScope == nil || info.Storage == nil
}
