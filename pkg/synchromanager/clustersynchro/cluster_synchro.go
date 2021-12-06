package clustersynchro

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	clustersv1alpha1 "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusters/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/legacyresource"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
)

type ClusterStatusUpdater interface {
	UpdateClusterStatus(ctx context.Context, name string, status *clustersv1alpha1.ClusterStatus) error
}

type ClusterSynchro struct {
	name string

	RESTConfig           *rest.Config
	ClusterStatusUpdater ClusterStatusUpdater

	restmapper           meta.RESTMapper
	clusterclient        kubernetes.Interface
	listerWatcherFactory informer.DynamicListerWatcherFactory

	storage                     storage.StorageFactory
	legacyResourceStorageConfig *legacyresource.StorageConfigFactory

	closeOnce sync.Once
	closer    chan struct{}
	closed    chan struct{}

	status chan struct{}

	runResourceSynchroCh  chan struct{}
	stopResourceSynchroCh chan struct{}

	resourcelock  sync.RWMutex
	handlerStopCh chan struct{}
	// Key is the storage resource.
	// Sometimes the synchronized resource and the storage resource are different
	resourceVersionCaches map[schema.GroupVersionResource]*informer.ResourceVersionStorage
	resourceSynchros      atomic.Value // map[schema.GroupVersionResource]*ResourceSynchro

	resourceStatuses atomic.Value // map[schema.GroupResource]*clustersv1alpha1.ClusterResourceStatus

	version        atomic.Value // version.Info
	readyCondition atomic.Value // metav1.Condition
}

func New(name string, config *rest.Config, storage storage.StorageFactory, updater ClusterStatusUpdater) (*ClusterSynchro, error) {
	clusterclient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	dynamaicclient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, err
	}

	resourceversions, err := storage.GetResourceVersions(context.TODO(), name)
	if err != nil {
		return nil, err
	}

	synchro := &ClusterSynchro{
		name:                 name,
		RESTConfig:           config,
		ClusterStatusUpdater: updater,
		storage:              storage,

		restmapper:                  mapper,
		clusterclient:               clusterclient,
		listerWatcherFactory:        informer.NewDynamicListWatcherFactory(dynamaicclient),
		legacyResourceStorageConfig: legacyresource.NewStorageConfigFactory(runtime.ContentTypeJSON),

		status: make(chan struct{}),
		closer: make(chan struct{}),
		closed: make(chan struct{}),

		runResourceSynchroCh:  make(chan struct{}),
		stopResourceSynchroCh: make(chan struct{}),

		resourceVersionCaches: make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage),
	}
	synchro.resourceSynchros.Store(map[schema.GroupVersionResource]*ResourceSynchro{})
	synchro.resourceStatuses.Store(map[schema.GroupResource]*clustersv1alpha1.ClusterResourceStatus{})

	condition := metav1.Condition{
		Type:               clustersv1alpha1.ClusterConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Pending",
		LastTransitionTime: metav1.Now(),
	}
	synchro.readyCondition.Store(condition)

	synchro.initWithResourceVersions(resourceversions)

	go synchro.Monitor()
	go synchro.clusterStatusUpdater()
	go synchro.resourceSynchroRunner()
	return synchro, nil
}

func (s *ClusterSynchro) initWithResourceVersions(resourceversions map[schema.GroupVersionResource]map[string]interface{}) {
	if len(resourceversions) == 0 {
		return
	}

	resourceVersionCaches := make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage, len(resourceversions))
	for gvr, rvs := range resourceversions {
		cache := informer.NewResourceVersionStorage(cache.DeletionHandlingMetaNamespaceKeyFunc)
		cache.Replace(rvs)
		resourceVersionCaches[gvr] = cache
	}

	s.resourceVersionCaches = resourceVersionCaches
}

type syncConfig struct {
	syncResource    schema.GroupVersionResource
	storageResource schema.GroupVersionResource
	convertor       runtime.ObjectConvertor
	storageConfig   *storage.ResourceStorageConfig
}

func (s *ClusterSynchro) SetResources(clusterResources []clustersv1alpha1.ClusterResource) {
	// configs key is resource's storage gvk
	configs := map[schema.GroupVersionResource]*syncConfig{}
	resourceStatuses := map[schema.GroupResource]*clustersv1alpha1.ClusterResourceStatus{}
	for _, resources := range clusterResources {
		for _, resource := range resources.Resources {
			gr := schema.GroupResource{Group: resources.Group, Resource: resource}
			gvks, err := s.restmapper.KindsFor(gr.WithVersion(""))
			if err != nil {
				klog.ErrorS(fmt.Errorf("Cluster not supported resource: %v", err), "Skip resource sync", "cluster", s.name, "resource", gr)
				continue
			}

			// filter cluster unsupported resource
			if len(gvks) == 0 {
				klog.InfoS("Skip resource sync, cluster not supported resource", "cluster", s.name, "resource", gr)
				continue
			}

			mapper, err := s.restmapper.RESTMapping(gvks[0].GroupKind(), gvks[0].Version)
			if err != nil {
				klog.ErrorS(err, "Skip resource sync", "cluster", s.name, "resource", gr)
				continue
			}

			// get cluster pedia supported versions
			gvs := legacyresource.Scheme.VersionsForGroupKind(gvks[0].GroupKind())
			if len(gvs) == 0 {
				// TODO(iceber): support custome resource
				klog.InfoS("Skip resource sync, not support to sync custom resource", "cluster", s.name, "resource", gr)
				continue

				/*
					// custome resource
					for _, version := range resources.Versions {
						gvrs[schema.GroupVersionResource{resources.Group, version, resource}] = struct{}{}
					}

					resourceStatuses[gr] = ResourceInfo{
						Kind:       mapper.GroupVersionKind.Kind,
						Namespaced: mapper.Scope.Name() == meta.RESTScopeNameNamespace,
					}
					continue
				*/
			}

			// kube resource
			var preferredVersion schema.GroupVersion

			// gvks is cluster supported versions
			// gvs is cluster pedia supported versions
			for _, gvk := range gvks {
				for _, gv := range gvs {
					if gvk.GroupVersion() == gv {
						preferredVersion = gv
					}
				}
			}

			// if not get preferred version, skip resource
			if preferredVersion.Empty() {
				klog.ErrorS(errors.New("Not found preferred version"), "Skip resource sync", "cluster", s.name, "resource", gr)
				continue
			}

			syncResource := preferredVersion.WithResource(resource)
			storageConfig, err := s.legacyResourceStorageConfig.NewConfig(syncResource)
			if err != nil {
				// TODO(iceber): set storage error ?
				klog.ErrorS(err, "Failed to create resource storage config", "cluster", s.name, "resource", syncResource)
				continue
			}
			storageResource := storageConfig.StorageGroupResource.WithVersion(storageConfig.StorageVersion.Version)
			if _, ok := configs[storageResource]; !ok {
				config := &syncConfig{
					syncResource:    syncResource,
					storageResource: storageResource,
					storageConfig:   storageConfig,
				}

				if syncResource != storageResource {
					config.convertor = legacyresource.Scheme
				}
				configs[storageResource] = config
			}

			info := &clustersv1alpha1.ClusterResourceStatus{
				Kind:       mapper.GroupVersionKind.Kind,
				Resource:   gr.Resource,
				Namespaced: mapper.Scope.Name() == meta.RESTScopeNameNamespace,
				SyncConditions: []clustersv1alpha1.ClusterResourceSyncCondition{
					{
						Version:        preferredVersion.Version,
						StorageVersion: storageConfig.StorageVersion.Version,
						Status:         clustersv1alpha1.SyncStatusPending,
						Reason:         "SynchroCreating",
					},
				},
			}
			if gr != storageConfig.StorageGroupResource {
				storageResource := storageConfig.StorageGroupResource.String()
				info.SyncConditions[0].StorageResource = &storageResource
			}
			resourceStatuses[gr] = info
		}
	}

	s.resourcelock.Lock()
	defer s.resourcelock.Unlock()
	s.resourceStatuses.Store(resourceStatuses)

	synchros := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)

	// filter deleted resources
	deleted := map[schema.GroupVersionResource]struct{}{}
	for gvr := range s.resourceVersionCaches {
		if _, ok := configs[gvr]; !ok {
			deleted[gvr] = struct{}{}
		}
	}

	// remove deleted resource synchro
	for gvr := range deleted {
		if handler, ok := synchros[gvr]; ok {
			handler.Close()
			delete(synchros, gvr)
		}

		if err := s.storage.CleanClusterResource(context.TODO(), s.name, gvr); err != nil {
			klog.ErrorS(err, "Failed to clean cluster resource", "cluster", s.name, "resource", gvr)
			// update resource sync status
			continue
		}

		delete(s.resourceVersionCaches, gvr)
	}

	for gvr, config := range configs {
		if _, ok := synchros[gvr]; ok {
			continue
		}

		resourceStorage, err := s.storage.NewResourceStorage(config.storageConfig)
		if err != nil {
			klog.ErrorS(err, "Failed to create resource storage", "cluster", s.name, "storage resource", config.storageResource)
			// update resource sync status
			continue
		}

		resourceVersionCache, ok := s.resourceVersionCaches[gvr]
		if !ok {
			resourceVersionCache = informer.NewResourceVersionStorage(cache.DeletionHandlingMetaNamespaceKeyFunc)
			s.resourceVersionCaches[gvr] = resourceVersionCache
		}

		synchro := newResourceSynchro(s.name,
			s.listerWatcherFactory.ForResource(metav1.NamespaceAll, config.syncResource),
			resourceVersionCache,
			config.convertor,
			resourceStorage,
		)
		if s.handlerStopCh != nil {
			select {
			case <-s.handlerStopCh:
			default:
				go synchro.Run(s.handlerStopCh)
			}
		}
		synchros[gvr] = synchro
	}
	s.resourceSynchros.Store(synchros)
}

func (s *ClusterSynchro) Shutdown() {
	s.closeOnce.Do(func() {
		close(s.closer)
	})

	synchros := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)
	for _, handler := range synchros {
		handler.Close()
	}

	<-s.closed
	close(s.status)
}

func (s *ClusterSynchro) genClusterStatus() *clustersv1alpha1.ClusterStatus {
	resourceStatuses := s.resourceStatuses.Load().(map[schema.GroupResource]*clustersv1alpha1.ClusterResourceStatus)
	synchros := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)

	groups := make(map[string]clustersv1alpha1.ClusterGroupStatus)
	for gr, resourceStatus := range resourceStatuses {
		groupStatus := groups[gr.Group]

		resourceStatus = resourceStatus.DeepCopy()
		for i, cond := range resourceStatus.SyncConditions {
			var gvr schema.GroupVersionResource
			if cond.StorageResource != nil {
				gvr = schema.ParseGroupResource(*cond.StorageResource).WithVersion(cond.StorageVersion)
			} else {
				gvr = gr.WithVersion(cond.StorageVersion)
			}

			if synchro, ok := synchros[gvr]; ok {
				status := synchro.Status()
				cond.Status = status.Status
				cond.Reason = status.Reason
				cond.Message = status.Message
				cond.LastTransitionTime = status.LastTransitionTime
			} else {
				if cond.Status == "" {
					cond.Status = clustersv1alpha1.SyncStatusPending
				}
				if cond.Reason == "" {
					cond.Reason = "SynchroNotFound"
				}
				if cond.Message == "" {
					cond.Message = "not found resource synchro"
				}
				cond.LastTransitionTime = metav1.Now()
			}

			resourceStatus.SyncConditions[i] = cond
		}

		groupStatus.Group = gr.Group
		groupStatus.Resources = append(groupStatus.Resources, *resourceStatus)
		groups[groupStatus.Group] = groupStatus
	}

	groupStatuses := make([]clustersv1alpha1.ClusterGroupStatus, 0, len(groups))
	for _, status := range groups {
		groupStatuses = append(groupStatuses, status)
	}
	sortClusterGroupStatusByName(groupStatuses)

	version := s.version.Load().(version.Info).GitVersion
	readyCondition := s.readyCondition.Load().(metav1.Condition)
	return &clustersv1alpha1.ClusterStatus{
		Version:    version,
		Conditions: []metav1.Condition{readyCondition},
		Resources:  groupStatuses,
	}
}

func (s *ClusterSynchro) updateStatus() {
	select {
	case s.status <- struct{}{}:
	default:
		return
	}
}

func (s *ClusterSynchro) clusterStatusUpdater() {
	for range s.status {
		status := s.genClusterStatus()
		if err := s.ClusterStatusUpdater.UpdateClusterStatus(context.TODO(), s.name, status); err != nil {
			klog.ErrorS(err, "Failed to update cluster status", "cluster", s.name, status.Conditions[0].Reason)
		}
	}
}

func (s *ClusterSynchro) resourceSynchroRunner() {
	defer close(s.closed)

	for {
		select {
		case <-s.runResourceSynchroCh:
		case <-s.closer:
			return
		}

		select {
		case <-s.stopResourceSynchroCh:
			continue
		case <-s.closer:
			return
		default:
		}

		s.resourcelock.Lock()
		s.handlerStopCh = make(chan struct{})
		go func() {
			select {
			case <-s.closer:
			case <-s.stopResourceSynchroCh:
			}

			close(s.handlerStopCh)
		}()

		handlers := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)
		for _, handler := range handlers {
			go handler.Run(s.handlerStopCh)
		}
		s.resourcelock.Unlock()

		<-s.handlerStopCh
	}
}

func (synchro *ClusterSynchro) startResourceSynchro() {
	select {
	case <-synchro.stopResourceSynchroCh:
		synchro.stopResourceSynchroCh = make(chan struct{})
	default:
	}

	select {
	case <-synchro.runResourceSynchroCh:
	default:
		close(synchro.runResourceSynchroCh)
	}
}

func (synchro *ClusterSynchro) stopResourceSynchro() {
	select {
	case <-synchro.runResourceSynchroCh:
		synchro.runResourceSynchroCh = make(chan struct{})
	default:
	}

	select {
	case <-synchro.stopResourceSynchroCh:
	default:
		close(synchro.stopResourceSynchroCh)
	}
}

func (synchro *ClusterSynchro) Monitor() {
	klog.V(2).InfoS("Cluster Synchro Monitor Running...", "cluster", synchro.name)

	wait.JitterUntil(synchro.checkClusterHealthy, 5*time.Second, 0.5, false, synchro.closer)
}

func (synchro *ClusterSynchro) checkClusterHealthy() {
	lastReadyCondition := synchro.readyCondition.Load().(metav1.Condition)
	ready, err := checkKubeHealthy(synchro.clusterclient)
	if ready {
		synchro.startResourceSynchro()

		if lastReadyCondition.Status != metav1.ConditionTrue {
			condition := metav1.Condition{
				Type:               clustersv1alpha1.ClusterConditionReady,
				Status:             metav1.ConditionTrue,
				Reason:             "Healthy",
				LastTransitionTime: metav1.Now(),
			}

			version, err := synchro.clusterclient.Discovery().ServerVersion()
			if err != nil {
				condition.Message = err.Error()
				klog.ErrorS(err, "Failed to get cluster version", "cluster", synchro.name)
			} else {
				synchro.version.Store(*version)
			}

			synchro.readyCondition.Store(condition)
		}

		synchro.updateStatus()
		return
	}

	condition := metav1.Condition{
		Type:   clustersv1alpha1.ClusterConditionReady,
		Status: metav1.ConditionFalse,
	}
	if err == nil {
		condition.Reason = "Unhealthy"
		condition.Message = "cluster health responded without ok"
	} else {
		condition.Reason = "NotReachable"
		condition.Message = err.Error()
	}
	if lastReadyCondition.Status != condition.Status || lastReadyCondition.Reason != condition.Reason || lastReadyCondition.Message != condition.Message {
		condition.LastTransitionTime = metav1.Now()
		synchro.readyCondition.Store(condition)
	}

	// if the last status was not ConditionTrue, stop resource synchros
	if lastReadyCondition.Status != metav1.ConditionTrue {
		synchro.stopResourceSynchro()
	}

	synchro.updateStatus()
}

// TODO(iceber): resolve for more detailed error
func checkKubeHealthy(client kubernetes.Interface) (bool, error) {
	ctx, cancel := context.WithTimeout(context.TODO(), 5*time.Second)
	defer cancel()

	_, err := client.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(ctx)
	if apierrors.IsNotFound(err) {
		_, err = client.Discovery().RESTClient().Get().AbsPath("/healthz").DoRaw(ctx)
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func sortClusterGroupStatusByName(statuses []clustersv1alpha1.ClusterGroupStatus) {
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Group < statuses[j].Group
	})
}
