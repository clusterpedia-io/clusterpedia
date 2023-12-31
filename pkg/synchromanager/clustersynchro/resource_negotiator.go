package clustersynchro

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/features"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

type ResourceNegotiator struct {
	name                   string
	dynamicDiscovery       discovery.DynamicDiscoveryInterface
	resourceStorageConfig  *storageconfig.StorageConfigFactory
	syncAllCustomResources bool
}

type syncConfig struct {
	kind          string
	syncResource  schema.GroupVersionResource
	convertor     runtime.ObjectConvertor
	storageConfig *storage.ResourceStorageConfig
}

func (negotiator *ResourceNegotiator) SetSyncAllCustomResources(sync bool) {
	negotiator.syncAllCustomResources = sync
}

func (negotiator *ResourceNegotiator) NegotiateSyncResources(syncResources []clusterv1alpha2.ClusterGroupResources) (*GroupResourceStatus, map[schema.GroupVersionResource]syncConfig) {
	var syncAllResources bool
	var watchKubeVersion, watchAggregatorResourceTypes bool
	for i, syncResource := range syncResources {
		if syncResource.Group == "*" {
			syncAllResources = true
			watchKubeVersion, watchAggregatorResourceTypes = true, true
			break
		}

		groupType := negotiator.dynamicDiscovery.GetGroupType(syncResource.Group)
		if groupType == discovery.AggregatorResource {
			watchAggregatorResourceTypes = true
		}

		for _, resource := range syncResource.Resources {
			if resource == "*" {
				syncResourcesByGroup := negotiator.dynamicDiscovery.GetGroupResourcesAsSyncResources(syncResource.Group)
				if syncResourcesByGroup == nil {
					syncResources[i].Resources = nil
					klog.InfoS("Skip resource sync", "cluster", negotiator.name, "group", syncResource.Group, "reason", "not match group")
				} else {
					syncResourcesByGroup.Versions = syncResource.Versions
					syncResources[i] = *syncResourcesByGroup
					if groupType == discovery.KubeResource {
						watchKubeVersion = true
					}
				}
				break
			}
		}
	}

	if syncAllResources {
		syncResources = negotiator.dynamicDiscovery.GetAllResourcesAsSyncResources()
	} else if negotiator.syncAllCustomResources && clusterpediafeature.FeatureGate.Enabled(features.AllowSyncAllCustomResources) {
		syncResources = negotiator.dynamicDiscovery.AttachAllCustomResourcesToSyncResources(syncResources)
	}

	// check for changes to the kube native resource types when the cluster version changes
	negotiator.dynamicDiscovery.WatchServerVersion(watchKubeVersion)
	negotiator.dynamicDiscovery.WatchAggregatorResourceTypes(watchAggregatorResourceTypes)

	var groupResourceStatus = NewGroupResourceStatus()
	var storageResourceSyncConfigs = make(map[schema.GroupVersionResource]syncConfig)
	for _, groupResources := range syncResources {
		for _, resource := range groupResources.Resources {
			syncGR := schema.GroupResource{Group: groupResources.Group, Resource: resource}

			if clusterpediafeature.FeatureGate.Enabled(features.IgnoreSyncLease) {
				// skip leases.coordination.k8s.io
				if syncGR.String() == "leases.coordination.k8s.io" {
					continue
				}
			}

			apiResource, supportedVersions := negotiator.dynamicDiscovery.GetAPIResourceAndVersions(syncGR)
			if apiResource == nil || len(supportedVersions) == 0 {
				continue
			}
			if !discovery.HasListAndWatchVerbs(*apiResource) {
				klog.InfoS("Skip resource sync", "cluster", negotiator.name, "resource", resource, "reason", "not support List and Watch", "verbs", apiResource.Verbs)
				continue
			}

			syncGK := schema.GroupKind{Group: syncGR.Group, Kind: apiResource.Kind}
			syncVersions, isLegacyResource, err := negotiateSyncVersions(syncGK, groupResources.Versions, supportedVersions)
			if err != nil {
				klog.InfoS("Skip resource sync", "cluster", negotiator.name, "resource", resource, "reason", err)
				continue
			}

			// resource is case-insensitive and can be singular or plural
			// set syncGR.Resource to plural
			syncGR.Resource = apiResource.Name

			groupResourceStatus.addResource(syncGR, apiResource.Kind, apiResource.Namespaced)
			for _, version := range syncVersions {
				syncGVR := syncGR.WithVersion(version)
				syncCondition := clusterv1alpha2.ClusterResourceSyncCondition{
					Version: syncGVR.Version,
					Status:  clusterv1alpha2.ResourceSyncStatusPending,
					Reason:  "SynchroCreating",
				}

				storageConfig, err := negotiator.resourceStorageConfig.NewConfig(syncGVR, apiResource.Namespaced)
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
				if isLegacyResource {
					convertor = scheme.LegacyResourceScheme
				} else {
					convertor = scheme.UnstructuredScheme
				}
				storageResourceSyncConfigs[storageGVR] = syncConfig{
					kind:          apiResource.Kind,
					syncResource:  syncGVR,
					storageConfig: storageConfig,
					convertor:     convertor,
				}
			}
		}
	}
	return groupResourceStatus, storageResourceSyncConfigs
}

func negotiateSyncVersions(kind schema.GroupKind, wantVersions []string, supportedVersions []string) ([]string, bool, error) {
	if len(supportedVersions) == 0 {
		return nil, false, errors.New("The supported versions are empty")
	}

	knowns := scheme.LegacyResourceScheme.VersionsForGroupKind(kind)
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

	wants := sets.New(wantVersions...)
	if wants.Has("*") {
		return supportedVersions, false, nil
	}

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

	versions       map[schema.GroupResource]sets.Set[string]
	syncConditions map[schema.GroupVersionResource]clusterv1alpha2.ClusterResourceSyncCondition
}

func NewGroupResourceStatus() *GroupResourceStatus {
	return &GroupResourceStatus{
		versions:       make(map[schema.GroupResource]sets.Set[string]),
		resources:      make(map[schema.GroupResource]clusterv1alpha2.ClusterResourceStatus),
		syncConditions: make(map[schema.GroupVersionResource]clusterv1alpha2.ClusterResourceSyncCondition),
	}
}

func (s *GroupResourceStatus) EnableConcurrent() {
	atomic.StoreInt32(&s.concurrent, 1)
}

func (s *GroupResourceStatus) DisableConcurrent() {
	atomic.StoreInt32(&s.concurrent, 0)
}

func (s *GroupResourceStatus) concurrentEnabled() bool {
	return atomic.LoadInt32(&s.concurrent) != 0
}

func (s *GroupResourceStatus) addResource(gr schema.GroupResource, kind string, namespaced bool) {
	if _, ok := s.resources[gr]; !ok {
		s.sortedGRs = append(s.sortedGRs, gr)
		s.versions[gr] = sets.Set[string]{}
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

		for _, version := range sets.List(s.versions[gr]) {
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

			s.versions[gr] = sets.New(other.versions[gr].UnsortedList()...)
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
