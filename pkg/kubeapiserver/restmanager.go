package kubeapiserver

import (
	"fmt"
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
	resourceconfigfactory "github.com/clusterpedia-io/clusterpedia/pkg/runtime/resourceconfig/factory"
	"github.com/clusterpedia-io/clusterpedia/pkg/runtime/scheme"
	unstructuredscheme "github.com/clusterpedia-io/clusterpedia/pkg/runtime/scheme/unstructured"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

// toDiscoveryKubeVerb maps an action.Verb to the logical kube verb, used for discovery
var toDiscoveryKubeVerb = map[string]string{
	"CONNECT":          "", // do not list in discovery.
	"DELETE":           "delete",
	"DELETECOLLECTION": "deletecollection",
	"GET":              "get",
	"LIST":             "list",
	"PATCH":            "patch",
	"POST":             "create",
	"PROXY":            "proxy",
	"PUT":              "update",
	"WATCH":            "watch",
	"WATCHLIST":        "watch",
}

type RESTManager struct {
	serializer                 runtime.NegotiatedSerializer
	storageFactory             storage.StorageFactory
	resourceConfigFactory      *resourceconfigfactory.ResourceConfigFactory
	equivalentResourceRegistry runtime.EquivalentResourceMapper

	lock      sync.Mutex
	groups    atomic.Value // map[string]metav1.APIGroup
	resources atomic.Value // map[schema.GroupResource]metav1.APIResource

	subresources map[schema.GroupResource]map[string]resourceRESTInfo

	resourceRESTInfos atomic.Value // map[schema.GroupVersionResource]resourceRESTInfo

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
		resourceConfigFactory:      resourceconfigfactory.New(),
		equivalentResourceRegistry: runtime.NewEquivalentResourceRegistry(),
		requestVerbs:               requestVerbs,
		subresources:               make(map[schema.GroupResource]map[string]resourceRESTInfo),
	}

	manager.resources.Store(apiresources)
	manager.groups.Store(map[string]metav1.APIGroup{})
	manager.resourceRESTInfos.Store(make(map[schema.GroupVersionResource]resourceRESTInfo))
	return manager
}

type subresource struct {
	name string

	// parent resource info
	gr         schema.GroupResource
	kind       string
	namespaced bool

	connecter interface {
		rest.Connecter
		rest.Storage
	}
}

// preRegisterSubresource is non-concurrently safe and only called at initialization time
func (m *RESTManager) preRegisterSubresource(subresource subresource) error {
	var verbs []string
	for _, method := range subresource.connecter.ConnectMethods() {
		if verb := toDiscoveryKubeVerb[method]; verb != "" {
			verbs = append(verbs, verb)
		}
	}
	if len(verbs) == 0 {
		return fmt.Errorf("resource['%s/%s'] available verbs are empty", subresource.gr.String(), subresource.name)
	}

	objGVK := subresource.connecter.(rest.Storage).New().GetObjectKind().GroupVersionKind()
	apiResource := metav1.APIResource{
		Name:       subresource.gr.Resource + "/" + subresource.name,
		Namespaced: subresource.namespaced,
		Verbs:      verbs,
		Kind:       objGVK.Kind,
	}

	namer := handlers.ContextBasedNaming{
		Namer:         runtime.Namer(meta.NewAccessor()),
		ClusterScoped: !subresource.namespaced,
	}

	requestScope := m.genLegacyResourceRequestScope(namer, subresource.gr.WithVersion(""), subresource.kind)
	requestScope.Subresource = subresource.name

	infos := m.subresources[subresource.gr]
	if infos == nil {
		infos = make(map[string]resourceRESTInfo)
		m.subresources[subresource.gr] = infos
	}
	infos[subresource.name] = resourceRESTInfo{
		APIResource:  apiResource,
		RequestScope: requestScope,
		Storage:      subresource.connecter,
	}
	return nil
}

func (m *RESTManager) GetAPIGroups() map[string]metav1.APIGroup {
	return m.groups.Load().(map[string]metav1.APIGroup)
}

func (m *RESTManager) GetResourceREST(gvr schema.GroupVersionResource, subresource string) (metav1.APIResource, *handlers.RequestScope, rest.Storage, bool) {
	if subresource != "" {
		infos, exited := m.subresources[gvr.GroupResource()]
		if !exited {
			return metav1.APIResource{}, nil, nil, false
		}
		info, exited := infos[subresource]
		if !exited {
			return metav1.APIResource{}, nil, nil, false
		}

		info.RequestScope.Resource = gvr
		info.RequestScope.Kind = info.RequestScope.Kind.GroupKind().WithVersion(gvr.Version)
		return info.APIResource, info.RequestScope, info.Storage, true
	}

	infos := m.resourceRESTInfos.Load().(map[schema.GroupVersionResource]resourceRESTInfo)
	info, exited := infos[gvr]
	if !exited {
		return metav1.APIResource{}, nil, nil, false
	}
	if info.Empty() {
		return metav1.APIResource{}, nil, nil, false
	}
	return info.APIResource, info.RequestScope, info.Storage, true
}

func (m *RESTManager) LoadResources(infos ResourceInfoMap) map[schema.GroupResource]discovery.ResourceDiscoveryAPI {
	apigroups := m.groups.Load().(map[string]metav1.APIGroup)
	apiresources := m.resources.Load().(map[schema.GroupResource]metav1.APIResource)
	restinfos := m.resourceRESTInfos.Load().(map[schema.GroupVersionResource]resourceRESTInfo)

	addedAPIGroups := make(map[string]metav1.APIGroup)
	addedAPIResources := make(map[schema.GroupResource]metav1.APIResource)
	addedInfos := make(map[schema.GroupVersionResource]resourceRESTInfo)

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
				addedInfos[gvr] = resourceRESTInfo{APIResource: resource}
			}
		}
		if infos, ok := m.subresources[gr]; ok {
			for sub, info := range infos {
				subapi := api
				subapi.Resource = info.APIResource
				apis[schema.GroupResource{Group: gr.Group, Resource: gr.Resource + "/" + sub}] = subapi
			}
		}
		apis[gr] = api
	}

	//  no new APIGroups or APIResources or resourceRESTInfo
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
		m.addResourceRESTInfosLocked(addedInfos)
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

func (m *RESTManager) addResourceRESTInfosLocked(addedInfos map[schema.GroupVersionResource]resourceRESTInfo) {
	restinfos := m.resourceRESTInfos.Load().(map[schema.GroupVersionResource]resourceRESTInfo)

	infos := make(map[schema.GroupVersionResource]resourceRESTInfo, len(restinfos)+len(addedInfos))
	for gvr, info := range restinfos {
		infos[gvr] = info
	}

	for gvr, info := range addedInfos {
		if _, ok := infos[gvr]; ok {
			continue
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

			info.RequestScope = requestScope
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
			info.RequestScope.TableConvertor = storage.TableConvertor
		}

		infos[gvr] = info
	}

	m.resourceRESTInfos.Store(infos)
}

func (m *RESTManager) genLegacyResourceRESTStorage(gvr schema.GroupVersionResource, kind string, namespaced bool) (*resourcerest.RESTStorage, error) {
	resourceConfig, err := m.resourceConfigFactory.NewLegacyResourceConfig(gvr.GroupResource(), namespaced)
	if err != nil {
		return nil, err
	}

	config := &storage.ResourceStorageConfig{ResourceConfig: *resourceConfig}
	resourceStorage, err := m.storageFactory.NewResourceStorage(config)
	if err != nil {
		return nil, err
	}

	return &resourcerest.RESTStorage{
		StorageGVR:               resourceConfig.StorageResource,
		DefaultQualifiedResource: gvr.GroupResource(),

		NewMemoryFunc: func() runtime.Object {
			obj, _ := scheme.LegacyResourceScheme.New(resourceConfig.MemoryResource.GroupVersion().WithKind(kind))
			return obj
		},
		NewMemoryListFunc: func() runtime.Object {
			obj, _ := scheme.LegacyResourceScheme.New(resourceConfig.MemoryResource.GroupVersion().WithKind(kind + "List"))
			return obj
		},

		NewStorageFunc: func() runtime.Object {
			obj, _ := scheme.LegacyResourceScheme.New(resourceConfig.StorageResource.GroupVersion().WithKind(kind))
			return obj
		},
		NewStorageListFunc: func() runtime.Object {
			obj, _ := scheme.LegacyResourceScheme.New(resourceConfig.StorageResource.GroupVersion().WithKind(kind + "List"))
			return obj
		},

		Storage: resourceStorage,
	}, nil
}

func (m *RESTManager) genUnstructuredRESTStorage(gvr schema.GroupVersionResource, kind string, namespaced bool) (*resourcerest.RESTStorage, error) {
	resourceConfig, err := m.resourceConfigFactory.NewUnstructuredConfig(gvr, namespaced)
	if err != nil {
		return nil, err
	}

	config := &storage.ResourceStorageConfig{ResourceConfig: *resourceConfig}
	resourceStorage, err := m.storageFactory.NewResourceStorage(config)
	if err != nil {
		return nil, err
	}

	return &resourcerest.RESTStorage{
		NewMemoryFunc: func() runtime.Object {
			obj := &unstructured.Unstructured{}
			obj.SetGroupVersionKind(resourceConfig.MemoryResource.GroupVersion().WithKind(kind))
			return obj
		},
		NewMemoryListFunc: func() runtime.Object {
			obj := &unstructured.UnstructuredList{}
			obj.SetGroupVersionKind(resourceConfig.MemoryResource.GroupVersion().WithKind(kind + "List"))
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

type resourceRESTInfo struct {
	APIResource  metav1.APIResource
	RequestScope *handlers.RequestScope
	Storage      rest.Storage
}

func (info resourceRESTInfo) Empty() bool {
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
