package clustersynchro

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type ResourceNegotiator struct {
	name                  string
	restmapper            meta.RESTMapper
	resourceStorageConfig *storageconfig.StorageConfigFactory

	customResourceController *CustomResourceController
}

type syncConfig struct {
	kind          string
	syncResource  schema.GroupVersionResource
	convertor     runtime.ObjectConvertor
	storageConfig *storage.ResourceStorageConfig
}

func (negotiator *ResourceNegotiator) NegotiateSyncResources(syncResources []clusterv1alpha2.ClusterGroupResources) (*GroupResourceStatus, map[schema.GroupVersionResource]syncConfig) {
	var groupResourceStatus = NewGroupResourceStatus()
	var storageResourceSyncConfigs = make(map[schema.GroupVersionResource]syncConfig)

	syncResources = negotiator.customResourceController.HandleSyncResources(syncResources)
	for _, groupResources := range syncResources {
		for _, resource := range groupResources.Resources {
			syncGR := schema.GroupResource{Group: groupResources.Group, Resource: resource}
			mapper, syncVersions, isLegacyResource := negotiator.negotiatorVersions(syncGR, groupResources.Versions)
			if mapper == nil || len(syncVersions) == 0 {
				continue
			}
			syncGR = mapper.Resource.GroupResource()

			groupResourceStatus.addResource(syncGR, mapper.GroupVersionKind.Kind, mapper.Scope.Name() == meta.RESTScopeNameNamespace)
			for _, version := range syncVersions {
				syncGVR := syncGR.WithVersion(version)
				syncCondition := clusterv1alpha2.ClusterResourceSyncCondition{
					Version: syncGVR.Version,
					Status:  clusterv1alpha2.SyncStatusPending,
					Reason:  "SynchroCreating",
				}

				storageConfig, err := negotiator.resourceStorageConfig.NewConfig(syncGVR)
				if err != nil {
					syncCondition.Reason = "SynchroCreateFailed"
					syncCondition.Message = fmt.Sprintf("new resource storage config failed: %s", err)
					groupResourceStatus.addSyncCondition(syncGVR, syncCondition)
					continue
				}

				storageGVR := storageConfig.StorageGroupResource.WithVersion(storageConfig.StorageVersion.Version)
				syncCondition.StorageVersion = storageGVR.Version
				if syncGR != storageConfig.StorageGroupResource {
					syncCondition.StorageResource = storageConfig.StorageGroupResource.String()
				}
				groupResourceStatus.addSyncCondition(syncGVR, syncCondition)

				if _, ok := storageResourceSyncConfigs[storageGVR]; ok {
					// if resource's storage resource has been synced, not need to sync this resource.
					continue
				}

				var convertor runtime.ObjectConvertor
				if syncGVR != storageGVR {
					if isLegacyResource {
						convertor = resourcescheme.LegacyResourceScheme
					} else {
						convertor = resourcescheme.CustomResourceScheme
					}
				}
				storageResourceSyncConfigs[storageGVR] = syncConfig{
					kind:          mapper.GroupVersionKind.Kind,
					syncResource:  syncGVR,
					storageConfig: storageConfig,
					convertor:     convertor,
				}
			}
		}
	}
	return groupResourceStatus, storageResourceSyncConfigs
}

func (negotiator *ResourceNegotiator) negotiatorVersions(resource schema.GroupResource, wantVersions []string) (*meta.RESTMapping, []string, bool) {
	var mapper *meta.RESTMapping
	var supportedVersions []string
	isLegacyResource := resourcescheme.LegacyResourceScheme.IsGroupRegistered(resource.Group)
	if !isLegacyResource {
		mapper, supportedVersions = negotiator.customResourceController.GetRESTMappingAndVersions(resource)
	}

	if mapper == nil {
		gvks, err := negotiator.restmapper.KindsFor(resource.WithVersion(""))
		if err != nil {
			klog.ErrorS(fmt.Errorf("Cluster not supported resource: %v", err), "Skip resource sync", "cluster", negotiator.name, "resource", resource)
			return nil, nil, isLegacyResource
		}

		var gk schema.GroupKind
		supportedVersions = make([]string, 0, len(gvks))
		for _, gvk := range gvks {
			// if syncGR.Group == "", gvk.Group maybe not equal to ""
			if gvk.Group == resource.Group {
				gk = gvk.GroupKind()
				supportedVersions = append(supportedVersions, gvk.Version)
			}
		}
		if len(supportedVersions) == 0 {
			klog.InfoS("Skip resource sync", "cluster", negotiator.name, "resource", resource, "reason", "The supported versions are empty")
			return nil, nil, isLegacyResource
		}

		mapper, err = negotiator.restmapper.RESTMapping(gk, supportedVersions[0])
		if err != nil {
			klog.ErrorS(err, "Skip resource sync", "cluster", negotiator.name, "resource", resource)
			return nil, nil, isLegacyResource
		}
	}

	syncVersions, isLegacyResource, err := negotiateSyncVersions(mapper.GroupVersionKind.GroupKind(), wantVersions, supportedVersions)
	if err != nil {
		klog.InfoS("Skip resource sync", "cluster", negotiator.name, "resource", resource, "reason", err)
		return nil, nil, isLegacyResource
	}
	return mapper, syncVersions, isLegacyResource
}

func negotiateSyncVersions(kind schema.GroupKind, wantVersions []string, supportedVersions []string) ([]string, bool, error) {
	if len(supportedVersions) == 0 {
		return nil, false, errors.New("The supported versions are empty")
	}

	knowns := resourcescheme.LegacyResourceScheme.VersionsForGroupKind(kind)
	if len(knowns) != 0 {
		for _, version := range supportedVersions {
			for _, gv := range knowns {
				if version == gv.Version {
					// preferred version
					return []string{version}, true, nil
				}
			}
		}

		// For legacy resources, only known version are synchronized,
		// and only one version is guaranteed to be synchronized and saved.
		return nil, true, errors.New("The supported versions do not contain any known versions")
	}

	var syncVersions []string
	if len(wantVersions) == 0 {
		syncVersions = supportedVersions
		if len(syncVersions) > 3 {
			syncVersions = syncVersions[:3]
		}
		return syncVersions, false, nil
	}

	wants := sets.NewString(wantVersions...)
	for _, version := range supportedVersions {
		if wants.Has(version) {
			syncVersions = append(syncVersions, version)
		}
	}

	if len(syncVersions) == 0 {
		return nil, false, errors.New("The supported versions do not contain any specified sync version")
	}
	return syncVersions, false, nil
}

// GroupResourceStatus manages the status of synchronized resources
// TODO: change to a more appropriate name
type GroupResourceStatus struct {
	concurrent int32
	lock       sync.RWMutex

	sortedGRs []schema.GroupResource
	resources map[schema.GroupResource]clusterv1alpha2.ClusterResourceStatus

	versions       map[schema.GroupResource]sets.String
	syncConditions map[schema.GroupVersionResource]clusterv1alpha2.ClusterResourceSyncCondition
}

func NewGroupResourceStatus() *GroupResourceStatus {
	return &GroupResourceStatus{
		versions:       make(map[schema.GroupResource]sets.String),
		resources:      make(map[schema.GroupResource]clusterv1alpha2.ClusterResourceStatus),
		syncConditions: make(map[schema.GroupVersionResource]clusterv1alpha2.ClusterResourceSyncCondition),
	}
}

func (s *GroupResourceStatus) EnableConcurrent() {
	atomic.StoreInt32(&s.concurrent, 0)
}

func (s *GroupResourceStatus) DisableConcurrent() {
	atomic.StoreInt32(&s.concurrent, 1)
}

func (s *GroupResourceStatus) concurrentEnabled() bool {
	return atomic.LoadInt32(&s.concurrent) != 0
}

func (s *GroupResourceStatus) addResource(gr schema.GroupResource, kind string, namespaced bool) {
	if _, ok := s.resources[gr]; !ok {
		s.sortedGRs = append(s.sortedGRs, gr)
		s.versions[gr] = sets.NewString()
	}

	s.resources[gr] = clusterv1alpha2.ClusterResourceStatus{
		Name:       gr.Resource,
		Kind:       kind,
		Namespaced: namespaced,
	}
}

func (s *GroupResourceStatus) addSyncCondition(gvr schema.GroupVersionResource, condition clusterv1alpha2.ClusterResourceSyncCondition) {
	if _, ok := s.resources[gvr.GroupResource()]; !ok {
		return
	}

	s.versions[gvr.GroupResource()].Insert(gvr.Version)
	s.syncConditions[gvr] = condition
}

func (s *GroupResourceStatus) UpdateSyncCondition(gvr schema.GroupVersionResource, status, reason, message string) {
	if s.concurrentEnabled() {
		s.lock.Lock()
		defer s.lock.Unlock()
	}

	cond, ok := s.syncConditions[gvr]
	if !ok {
		return
	}

	cond.Status, cond.Reason, cond.Message = status, reason, message
	s.syncConditions[gvr] = cond
}

func (s *GroupResourceStatus) DeleteVersion(gvr schema.GroupVersionResource) {
	if s.concurrentEnabled() {
		s.lock.Lock()
		defer s.lock.Unlock()
	}

	gr := gvr.GroupResource()
	if _, ok := s.resources[gr]; !ok {
		return
	}

	delete(s.syncConditions, gvr)
	s.versions[gr].Delete(gvr.Version)

	if len(s.versions[gr]) == 0 {
		delete(s.resources, gr)
		delete(s.versions, gr)
	}
}

func (s *GroupResourceStatus) LoadGroupResourcesStatuses() []clusterv1alpha2.ClusterGroupResourcesStatus {
	if s.concurrentEnabled() {
		s.lock.RLock()
		defer s.lock.RUnlock()
	}

	indexs := make(map[string]int)
	groupStatuses := make([]clusterv1alpha2.ClusterGroupResourcesStatus, 0)
	for _, gr := range s.sortedGRs {
		resource, ok := s.resources[gr]
		if !ok {
			continue
		}

		for _, version := range s.versions[gr].List() {
			resource.SyncConditions = append(resource.SyncConditions, s.syncConditions[gr.WithVersion(version)])
		}

		index, ok := indexs[gr.Group]
		if !ok {
			groupStatuses = append(groupStatuses, clusterv1alpha2.ClusterGroupResourcesStatus{Group: gr.Group})
			index = len(groupStatuses) - 1
			indexs[gr.Group] = index
		}
		groupStatuses[index].Resources = append(groupStatuses[index].Resources, resource)
	}
	return groupStatuses
}

func (s *GroupResourceStatus) GetStorageGVRToSyncGVRs() map[schema.GroupVersionResource]GVRSet {
	if s.concurrentEnabled() {
		s.lock.RLock()
		defer s.lock.RUnlock()
	}

	gvrMap := make(map[schema.GroupVersionResource]GVRSet)
	for gvr, cond := range s.syncConditions {
		storageGVR := cond.StorageGVR(gvr.GroupResource())
		if storageGVR.Empty() {
			continue
		}

		if gvrs, ok := gvrMap[storageGVR]; ok {
			gvrs.Insert(gvr)
		} else {
			gvrMap[storageGVR] = NewGVRSet(gvr)
		}
	}
	return gvrMap
}

func (s *GroupResourceStatus) Merge(other *GroupResourceStatus) GVRSet {
	if other == nil {
		return nil
	}

	if s.concurrentEnabled() {
		s.lock.Lock()
		defer s.lock.Unlock()
	}

	if other.concurrentEnabled() {
		other.lock.RLock()
		defer other.lock.RUnlock()
	}

	if reflect.DeepEqual(s.versions, other.versions) {
		return nil
	}

	addition := NewGVRSet()
	for gr, resource := range other.resources {
		if len(other.versions[gr]) == 0 {
			// if other's gr not have versions, ignore
			continue
		}

		if _, ok := s.resources[gr]; !ok {
			// s don't have gr, add gr and gr's versions
			s.sortedGRs = append(s.sortedGRs, gr)
			s.resources[gr] = resource

			s.versions[gr] = sets.NewString(other.versions[gr].UnsortedList()...)
			for version := range other.versions[gr] {
				gvr := gr.WithVersion(version)
				s.syncConditions[gvr] = other.syncConditions[gvr]

				addition.Insert(gvr)
			}
			continue
		}

		// s have gr, find versions not in s
		for version := range other.versions[gr] {
			gvr := gr.WithVersion(version)
			if _, ok := s.syncConditions[gvr]; ok {
				continue
			}

			s.versions[gr].Insert(version)
			s.syncConditions[gvr] = other.syncConditions[gvr]

			addition.Insert(gvr)
		}
	}
	return addition
}

type GVRSet map[schema.GroupVersionResource]struct{}

func NewGVRSet(gvrs ...schema.GroupVersionResource) GVRSet {
	set := make(GVRSet)
	set.Insert(gvrs...)
	return set
}

func (set GVRSet) Insert(gvrs ...schema.GroupVersionResource) {
	for _, gvr := range gvrs {
		set[gvr] = struct{}{}
	}
}
