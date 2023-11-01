package kubeapiserver

import (
	"strings"
	"sync"
	"sync/atomic"

	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/runtime/serializer/versioning"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/klog/v2"
	apicore "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/apis/rbac"
	apisstorage "k8s.io/kubernetes/pkg/apis/storage"
	printersinternal "k8s.io/kubernetes/pkg/printers/internalversion"
	printerstorage "k8s.io/kubernetes/pkg/printers/storage"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/printers"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcerest"
	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
	unstructuredscheme "github.com/clusterpedia-io/clusterpedia/pkg/scheme/unstructured"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/storageconfig"
)

type RESTManager struct {
	serializer                 runtime.NegotiatedSerializer
	storageFactory             storage.StorageFactory
	resourcetSorageConfig      *storageconfig.StorageConfigFactory
	equivalentResourceRegistry runtime.EquivalentResourceMapper

	lock      sync.Mutex
	groups    atomic.Value // map[string]metav1.APIGroup
	resources atomic.Value // map[schema.GroupResource]metav1.APIResource

	restResourceInfos atomic.Value // map[schema.GroupVersionResource]RESTResourceInfo

	requestVerbs metav1.Verbs
}

func NewRESTManager(serializer runtime.NegotiatedSerializer, storageMediaType string, storageFactory storage.StorageFactory, initialAPIGroupResources []*restmapper.APIGroupResources) *RESTManager {
	requestVerbs := storageFactory.GetSupportedRequestVerbs()

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
				if gvs := scheme.LegacyResourceScheme.VersionsForGroupKind(gk); len(gvs) == 0 {
					// skip custom resource
					continue
				}

				resource.Verbs = requestVerbs
				apiresources[gr] = resource
			}
		}
	}

	manager := &RESTManager{
		serializer:                 serializer,
		storageFactory:             storageFactory,
		resourcetSorageConfig:      storageconfig.NewStorageConfigFactory(),
		equivalentResourceRegistry: runtime.NewEquivalentResourceRegistry(),
		requestVerbs:               requestVerbs,
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
				for _, gv := range scheme.LegacyResourceScheme.PrioritizedVersionsForGroup(gr.Group) {
					group.Versions = append(group.Versions, metav1.GroupVersionForDiscovery{
						GroupVersion: gv.String(),
						Version:      gv.Version,
					})
				}
				addedAPIGroups[group.Name] = group
			}
		}

		gk := schema.GroupKind{Group: gr.Group, Kind: info.Kind}
		versions := scheme.LegacyResourceScheme.VersionsForGroupKind(gk)
		if len(versions) == 0 {
			// custom resource
			for version := range info.Versions {
				versions = append(versions, schema.GroupVersion{Group: gr.Group, Version: version})
			}
		}

		resource, hasResource := apiresources[gr]
		if !hasResource {
			resource = metav1.APIResource{Name: gr.Resource, Namespaced: info.Namespaced, Kind: info.Kind}
			resource.Verbs = m.requestVerbs
			addedAPIResources[gr] = resource
		}

		api := discovery.ResourceDiscoveryAPI{
			Group:    gr.Group,
			Resource: resource,
			Versions: make(sets.Set[schema.GroupVersion], len(versions)),
		}
		for _, version := range versions {
			api.Versions.Insert(version)

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
			var err error
			var storage *resourcerest.RESTStorage
			if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
				storage, err = m.genLegacyResourceRESTStorage(gvr, info.APIResource.Kind, info.APIResource.Namespaced)
			} else {
				storage, err = m.genUnstructuredRESTStorage(gvr, info.APIResource.Kind, info.APIResource.Namespaced)
			}
			if err != nil {
				klog.ErrorS(err, "Failed to gen resource rest storage", "gvr", gvr, "kind", info.APIResource.Kind)
				continue
			}

			storage.DefaultQualifiedResource = gvr.GroupResource()
			storage.TableConvertor = GetTableConvertor(gvr.GroupResource())
			storage.Serializer = m.serializer
			info.Storage = storage
		}

		if info.RequestScope == nil {
			namer := handlers.ContextBasedNaming{
				Namer:         runtime.Namer(meta.NewAccessor()),
				ClusterScoped: !info.APIResource.Namespaced,
			}

			var requestScope *handlers.RequestScope
			if scheme.LegacyResourceScheme.IsGroupRegistered(gvr.Group) {
				requestScope = m.genLegacyResourceRequestScope(namer, gvr, info.APIResource.Kind)
			} else {
				requestScope = m.genUnstructuredRequestScope(namer, gvr, info.APIResource.Kind)
			}

			requestScope.TableConvertor = info.Storage
			info.RequestScope = requestScope
		}

		infos[gvr] = info
	}

	m.restResourceInfos.Store(infos)
}

func (m *RESTManager) genLegacyResourceRESTStorage(gvr schema.GroupVersionResource, kind string, namespaced bool) (*resourcerest.RESTStorage, error) {
	storageConfig, err := m.resourcetSorageConfig.NewLegacyResourceConfig(gvr.GroupResource(), namespaced)
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
			obj, _ := scheme.LegacyResourceScheme.New(storageConfig.MemoryVersion.WithKind(kind))
			return obj
		},
		NewListFunc: func() runtime.Object {
			obj, _ := scheme.LegacyResourceScheme.New(storageConfig.MemoryVersion.WithKind(kind + "List"))
			return obj
		},

		Storage: resourceStorage,
	}, nil
}

func (m *RESTManager) genUnstructuredRESTStorage(gvr schema.GroupVersionResource, kind string, namespaced bool) (*resourcerest.RESTStorage, error) {
	storageConfig, err := m.resourcetSorageConfig.NewUnstructuredConfig(gvr, namespaced)
	if err != nil {
		return nil, err
	}

	resourceStorage, err := m.storageFactory.NewResourceStorage(storageConfig)
	if err != nil {
		return nil, err
	}

	return &resourcerest.RESTStorage{
		NewFunc: func() runtime.Object {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(storageConfig.MemoryVersion.WithKind(kind))
			return obj
		},
		NewListFunc: func() runtime.Object {
			obj := &unstructured.UnstructuredList{}
			obj.SetGroupVersionKind(storageConfig.MemoryVersion.WithKind(kind + "List"))
			return obj
		},

		Storage: resourceStorage,
	}, nil
}

// TODO: support custom resource
func (m *RESTManager) genLegacyResourceRequestScope(namer handlers.ScopeNamer, gvr schema.GroupVersionResource, kind string) *handlers.RequestScope {
	return &handlers.RequestScope{
		Namer:          namer,
		Serializer:     scheme.LegacyResourceCodecs,
		ParameterCodec: scheme.LegacyResourceParameterCodec,
		Creater:        scheme.LegacyResourceScheme,
		Convertor:      scheme.LegacyResourceScheme,
		Defaulter:      scheme.LegacyResourceScheme,
		Typer:          scheme.LegacyResourceScheme,

		Resource:         gvr,
		Kind:             gvr.GroupVersion().WithKind(kind),
		MetaGroupVersion: metav1.SchemeGroupVersion,

		EquivalentResourceMapper: m.equivalentResourceRegistry,
	}
}

func (m *RESTManager) genUnstructuredRequestScope(namer handlers.ScopeNamer, gvr schema.GroupVersionResource, kind string) *handlers.RequestScope {
	parameterScheme := runtime.NewScheme()
	parameterScheme.AddUnversionedTypes(schema.GroupVersion{Group: gvr.Group, Version: gvr.Version},
		&metav1.ListOptions{},
		&metav1.GetOptions{},
		&metav1.DeleteOptions{},
	)
	parameterCodec := runtime.NewParameterCodec(parameterScheme)

	typer := unstructuredObjectTyper{parameterScheme, scheme.UnstructuredScheme}
	negotiatedSerializer := unstructuredNegotiatedSerializer{
		typer,
		scheme.UnstructuredScheme,
		scheme.UnstructuredScheme,
	}

	var standardSerializers []runtime.SerializerInfo
	for _, s := range negotiatedSerializer.SupportedMediaTypes() {
		if s.MediaType == runtime.ContentTypeProtobuf {
			continue
		}
		standardSerializers = append(standardSerializers, s)
	}

	return &handlers.RequestScope{
		Namer:               namer,
		Serializer:          negotiatedSerializer,
		StandardSerializers: standardSerializers,
		ParameterCodec:      parameterCodec,
		Creater:             scheme.UnstructuredScheme,
		Convertor:           scheme.UnstructuredScheme,
		UnsafeConvertor:     unstructuredscheme.UnsafeObjectConvertor(scheme.UnstructuredScheme),
		Defaulter:           scheme.UnstructuredScheme,
		Typer:               typer,

		Resource: gvr,
		Kind:     gvr.GroupVersion().WithKind(kind),
		// HubGroupVersion:  gvr.GroupVersion(),
		MetaGroupVersion: metav1.SchemeGroupVersion,

		EquivalentResourceMapper: m.equivalentResourceRegistry,
	}
}

// from https://github.com/kubernetes/apiextensions-apiserver/blob/d79a59d4f63006d9c91bff1c417b82a54c3daeb4/pkg/apiserver/customresource_handler.go#L1029
type unstructuredNegotiatedSerializer struct {
	typer     runtime.ObjectTyper
	creater   runtime.ObjectCreater
	convertor runtime.ObjectConvertor
}

func (s unstructuredNegotiatedSerializer) SupportedMediaTypes() []runtime.SerializerInfo {
	return []runtime.SerializerInfo{
		{
			MediaType:        "application/json",
			MediaTypeType:    "application",
			MediaTypeSubType: "json",
			EncodesAsText:    true,
			Serializer:       json.NewSerializer(json.DefaultMetaFactory, s.creater, s.typer, false),
			PrettySerializer: json.NewSerializer(json.DefaultMetaFactory, s.creater, s.typer, true),
			StreamSerializer: &runtime.StreamSerializerInfo{
				EncodesAsText: true,
				Serializer:    json.NewSerializer(json.DefaultMetaFactory, s.creater, s.typer, false),
				Framer:        json.Framer,
			},
		},
		{
			MediaType:        "application/yaml",
			MediaTypeType:    "application",
			MediaTypeSubType: "yaml",
			EncodesAsText:    true,
			Serializer:       json.NewYAMLSerializer(json.DefaultMetaFactory, s.creater, s.typer),
		},
		{
			MediaType:        "application/vnd.kubernetes.protobuf",
			MediaTypeType:    "application",
			MediaTypeSubType: "vnd.kubernetes.protobuf",
			Serializer:       protobuf.NewSerializer(s.creater, s.typer),
			StreamSerializer: &runtime.StreamSerializerInfo{
				Serializer: protobuf.NewRawSerializer(s.creater, s.typer),
				Framer:     protobuf.LengthDelimitedFramer,
			},
		},
	}
}

func (s unstructuredNegotiatedSerializer) EncoderForVersion(encoder runtime.Encoder, gv runtime.GroupVersioner) runtime.Encoder {
	return versioning.NewCodec(encoder, nil, s.convertor, Scheme, Scheme, Scheme, gv, nil, "unstructuredNegotiatedSerializer")
}

func (s unstructuredNegotiatedSerializer) DecoderToVersion(decoder runtime.Decoder, gv runtime.GroupVersioner) runtime.Decoder {
	return versioning.NewCodec(nil, decoder, runtime.UnsafeObjectConvertor(Scheme), Scheme, Scheme, nil, nil, gv, "unstructuredNegotiatedSerializer")
}

// from https://github.com/kubernetes/apiextensions-apiserver/blob/d79a59d4f63006d9c91bff1c417b82a54c3daeb4/pkg/apiserver/customresource_handler.go#L1087
type unstructuredObjectTyper struct {
	Delegate          runtime.ObjectTyper
	UnstructuredTyper runtime.ObjectTyper
}

func (t unstructuredObjectTyper) ObjectKinds(obj runtime.Object) ([]schema.GroupVersionKind, bool, error) {
	// Delegate for things other than Unstructured.
	if _, ok := obj.(runtime.Unstructured); !ok {
		return t.Delegate.ObjectKinds(obj)
	}
	return t.UnstructuredTyper.ObjectKinds(obj)
}

func (t unstructuredObjectTyper) Recognizes(gvk schema.GroupVersionKind) bool {
	return t.Delegate.Recognizes(gvk) || t.UnstructuredTyper.Recognizes(gvk)
}

type RESTResourceInfo struct {
	APIResource  metav1.APIResource
	RequestScope *handlers.RequestScope
	Storage      *resourcerest.RESTStorage
}

func (info RESTResourceInfo) Empty() bool {
	return info.APIResource.Name == "" || info.RequestScope == nil || info.Storage == nil
}

var legacyResourcesWithDefaultTableConvertor = map[schema.GroupResource]struct{}{
	apicore.Resource("limitranges"):                     {},
	rbac.Resource("roles"):                              {},
	rbac.Resource("clusterroles"):                       {},
	apisstorage.Resource("csistoragecapacities"):        {},
	apiextensions.Resource("customresourcedefinitions"): {},
}

func GetTableConvertor(gr schema.GroupResource) rest.TableConvertor {
	if !scheme.LegacyResourceScheme.IsGroupRegistered(gr.Group) {
		return printers.NewDefaultTableConvertor(gr)
	}

	if _, ok := legacyResourcesWithDefaultTableConvertor[gr]; ok {
		return printers.NewDefaultTableConvertor(gr)
	}

	return printerstorage.TableConvertor{TableGenerator: printers.NewClusterTableGenerator().With(printersinternal.AddHandlers, printers.AddAPIServiceHandler)}
}
