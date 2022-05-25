package clustersynchro

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/discovery"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
)

type ClusterSynchro struct {
	name string

	RESTConfig           *rest.Config
	ClusterStatusUpdater ClusterStatusUpdater

	storage              storage.StorageFactory
	clusterclient        kubernetes.Interface
	listerWatcherFactory informer.DynamicListerWatcherFactory

	closeOnce sync.Once
	closer    chan struct{}
	closed    chan struct{}

	updateStatusCh        chan struct{}
	runResourceSynchroCh  chan struct{}
	stopResourceSynchroCh chan struct{}

	waitGroup wait.Group

	resourceSynchroLock sync.RWMutex
	handlerStopCh       chan struct{}
	// Key is the storage resource.
	// Sometimes the synchronized resource and the storage resource are different
	storageResourceVersionCaches map[schema.GroupVersionResource]*informer.ResourceVersionStorage
	storageResourceSynchros      sync.Map

	crdController           *CRDController
	apiServiceController    *APIServiceController
	dynamicDiscoveryManager *discovery.DynamicDiscoveryManager

	syncResources       atomic.Value // []clusterv1alpha2.ClusterGroupResources
	setSyncResourcesCh  chan struct{}
	resourceNegotiator  *ResourceNegotiator
	groupResourceStatus atomic.Value // *GroupResourceStatus

	readyCondition atomic.Value // metav1.Condition
}

type ClusterStatusUpdater interface {
	UpdateClusterStatus(ctx context.Context, name string, status *clusterv1alpha2.ClusterStatus) error
}

func New(name string, config *rest.Config, storage storage.StorageFactory, updater ClusterStatusUpdater) (*ClusterSynchro, error) {
	clusterclient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create a cluster client: %w", err)
	}

	dynamicDiscoveryManager, err := discovery.NewDynamicDiscoveryManager(name, clusterclient.Discovery())
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic discovery manager: %w", err)
	}

	_, crdVersions := dynamicDiscoveryManager.GetAPIResourceAndVersions(schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"})
	if len(crdVersions) == 0 {
		return nil, fmt.Errorf("not match crd version")
	}

	crdController, err := NewCRDController(name, config, crdVersions[0], dynamicDiscoveryManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create crd controller: %w", err)
	}

	apiServiceController, err := NewAPIServiceController(name, config, dynamicDiscoveryManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create apiservice controller: %w", err)
	}

	listWatchFactory, err := informer.NewDynamicListerWatcherFactory(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create lister watcher factory: %w", err)
	}

	resourceversions, err := storage.GetResourceVersions(context.TODO(), name)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource versions from storage: %w", err)
	}

	synchro := &ClusterSynchro{
		name:                 name,
		RESTConfig:           config,
		ClusterStatusUpdater: updater,
		storage:              storage,

		clusterclient:        clusterclient,
		listerWatcherFactory: listWatchFactory,

		dynamicDiscoveryManager: dynamicDiscoveryManager,
		crdController:           crdController,
		apiServiceController:    apiServiceController,

		closer: make(chan struct{}),
		closed: make(chan struct{}),

		updateStatusCh:        make(chan struct{}, 1),
		runResourceSynchroCh:  make(chan struct{}),
		stopResourceSynchroCh: make(chan struct{}),

		storageResourceVersionCaches: make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage),
	}

	synchro.resourceNegotiator = &ResourceNegotiator{
		name:                  name,
		resourceStorageConfig: storageconfig.NewStorageConfigFactory(),
		discoveryManager:      dynamicDiscoveryManager,
	}
	synchro.groupResourceStatus.Store((*GroupResourceStatus)(nil))

	synchro.syncResources.Store([]clusterv1alpha2.ClusterGroupResources(nil))
	synchro.setSyncResourcesCh = make(chan struct{}, 1)

	condition := metav1.Condition{
		Type:               clusterv1alpha2.ClusterReadyCondition,
		Status:             metav1.ConditionFalse,
		Reason:             clusterv1alpha2.PendingReason,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.readyCondition.Store(condition)

	synchro.initWithResourceVersions(resourceversions)
	return synchro, nil
}

func (s *ClusterSynchro) initWithResourceVersions(resourceversions map[schema.GroupVersionResource]map[string]interface{}) {
	if len(resourceversions) == 0 {
		return
	}

	storageResourceVersionCaches := make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage, len(resourceversions))
	for gvr, rvs := range resourceversions {
		cache := informer.NewResourceVersionStorage()
		_ = cache.Replace(rvs)
		storageResourceVersionCaches[gvr] = cache
	}

	s.storageResourceVersionCaches = storageResourceVersionCaches
}

func (s *ClusterSynchro) Run(shutdown <-chan struct{}) {
	// TODO(iceber): The start and stop of dynamicDiscoveryManager, crdController, apiServiceController
	// should be controlled by cluster healthy.
	go s.dynamicDiscoveryManager.Run(s.closer)
	go func() {
		// First initialize the information for the custom resources to dynamic discovery manager
		go s.crdController.Run(s.closer)
		if !cache.WaitForNamedCacheSync(s.name+"-CRD-Controller", s.closer, s.crdController.HasSynced) {
			return
		}

		go s.apiServiceController.Run(s.closer)
		if !cache.WaitForNamedCacheSync(s.name+"-APIService-Controllers", s.closer, s.apiServiceController.HasSynced) {
			return
		}

		s.dynamicDiscoveryManager.SetResourceMutationHandler(s.resetSyncResources)
		s.resetSyncResources()

		go s.syncResourcesSetter()
	}()

	s.waitGroup.Start(s.monitor)
	s.waitGroup.Start(s.resourceSynchroRunner)
	go s.clusterStatusUpdater()

	select {
	case <-shutdown:
		s.Shutdown(true)
	case <-s.closer:
	}
	<-s.closed
}

func (s *ClusterSynchro) Shutdown(updateReadyCondition bool) {
	s.closeOnce.Do(func() {
		close(s.closer)
	})

	s.waitGroup.Wait()

	if updateReadyCondition {
		var message string
		lastReadyCondition := s.readyCondition.Load().(metav1.Condition)
		if lastReadyCondition.Status == metav1.ConditionFalse {
			message = fmt.Sprintf("Last Condition Reason: %s, Message: %s", lastReadyCondition.Reason, lastReadyCondition.Message)
		}
		condition := metav1.Condition{
			Type:               clusterv1alpha2.ClusterReadyCondition,
			Status:             metav1.ConditionFalse,
			Reason:             clusterv1alpha2.ClusterSynchroStopReason,
			Message:            message,
			LastTransitionTime: metav1.Now(),
		}
		s.readyCondition.Store(condition)

		s.updateStatus()
	}

	close(s.updateStatusCh)
	<-s.closed
}

func (s *ClusterSynchro) SetResources(syncResources []clusterv1alpha2.ClusterGroupResources, syncAllCustomResources bool) {
	s.syncResources.Store(syncResources)
	s.resourceNegotiator.SetSyncAllCustomResources(syncAllCustomResources)
	s.resetSyncResources()
}

func (s *ClusterSynchro) resetSyncResources() {
	select {
	case s.setSyncResourcesCh <- struct{}{}:
	default:
	}
}

func (s *ClusterSynchro) syncResourcesSetter() {
	for {
		select {
		case <-s.setSyncResourcesCh:
			s.setSyncResources()
		case <-s.closer:
			return
		}
	}
}

func (s *ClusterSynchro) setSyncResources() {
	syncResources := s.syncResources.Load().([]clusterv1alpha2.ClusterGroupResources)
	if syncResources == nil {
		return
	}

	groupResourceStatus, storageResourceSyncConfigs := s.resourceNegotiator.NegotiateSyncResources(syncResources)

	lastGroupResourceStatus := s.groupResourceStatus.Load().(*GroupResourceStatus)
	deleted := groupResourceStatus.Merge(lastGroupResourceStatus)

	groupResourceStatus.EnableConcurrent()
	defer groupResourceStatus.DisableConcurrent()
	s.groupResourceStatus.Store(groupResourceStatus)

	// multiple resources may match the same storage resource
	storageGVRToSyncGVRs := groupResourceStatus.GetStorageGVRToSyncGVRs()
	updateSyncConditions := func(storageGVR schema.GroupVersionResource, status, reason, message string) {
		for gvr := range storageGVRToSyncGVRs[storageGVR] {
			groupResourceStatus.UpdateSyncCondition(gvr, status, reason, message)
		}
	}

	func() {
		s.resourceSynchroLock.Lock()
		defer s.resourceSynchroLock.Unlock()

		for storageGVR, config := range storageResourceSyncConfigs {
			// TODO: if config is changed, don't update resource synchro
			if _, ok := s.storageResourceSynchros.Load(storageGVR); ok {
				continue
			}

			resourceStorage, err := s.storage.NewResourceStorage(config.storageConfig)
			if err != nil {
				klog.ErrorS(err, "Failed to create resource storage", "cluster", s.name, "storage resource", storageGVR)
				updateSyncConditions(storageGVR, clusterv1alpha2.SyncStatusPending, "SynchroCreateFailed", fmt.Sprintf("new resource storage failed: %s", err))
				continue
			}

			resourceVersionCache, ok := s.storageResourceVersionCaches[storageGVR]
			if !ok {
				resourceVersionCache = informer.NewResourceVersionStorage()
				s.storageResourceVersionCaches[storageGVR] = resourceVersionCache
			}

			synchro := newResourceSynchro(
				s.name,
				config.syncResource,
				config.kind,
				s.listerWatcherFactory.ForResource(metav1.NamespaceAll, config.syncResource),
				resourceVersionCache,
				config.convertor,
				resourceStorage,
			)
			s.waitGroup.StartWithChannel(s.closer, synchro.Run)
			s.storageResourceSynchros.Store(storageGVR, synchro)

			// After the synchronizer is successfully created,
			// clean up the reasons and message initialized in the sync condition
			updateSyncConditions(storageGVR, clusterv1alpha2.SyncStatusUnknown, "", "")

			if s.handlerStopCh != nil {
				select {
				case <-s.handlerStopCh:
				default:
					go synchro.Start(s.handlerStopCh)
				}
			}
		}
	}()

	// close unsynced resource synchros
	removedStorageGVRs := NewGVRSet()
	s.storageResourceSynchros.Range(func(key, _ interface{}) bool {
		storageGVR := key.(schema.GroupVersionResource)
		if _, ok := storageResourceSyncConfigs[storageGVR]; !ok {
			removedStorageGVRs.Insert(storageGVR)
		}
		return true
	})
	for storageGVR := range removedStorageGVRs {
		if synchro, ok := s.storageResourceSynchros.Load(storageGVR); ok {
			select {
			case <-synchro.(*ResourceSynchro).Close():
			case <-s.closer:
				return
			}

			updateSyncConditions(storageGVR, clusterv1alpha2.SyncStatusStop, "SynchroRemoved", "the resource synchro is moved")
			s.storageResourceSynchros.Delete(storageGVR)
		}
	}

	// clean up unstoraged resources
	for storageGVR := range s.storageResourceVersionCaches {
		if _, ok := storageResourceSyncConfigs[storageGVR]; ok {
			continue
		}

		// Whether the storage resource is cleaned successfully or not, it needs to be deleted from `s.storageResourceVersionCaches`
		delete(s.storageResourceVersionCaches, storageGVR)

		err := s.storage.CleanClusterResource(context.TODO(), s.name, storageGVR)
		if err == nil {
			continue
		}

		// even if err != nil, the resource may have been cleaned up
		klog.ErrorS(err, "Failed to clean cluster resource", "cluster", s.name, "storage resource", storageGVR)
		updateSyncConditions(storageGVR, clusterv1alpha2.SyncStatusStop, "CleanResourceFailed", err.Error())
		for gvr := range storageGVRToSyncGVRs[storageGVR] {
			// not delete failed gvr
			delete(deleted, gvr)
		}
	}

	for gvr := range deleted {
		groupResourceStatus.DeleteVersion(gvr)
	}
}

func (s *ClusterSynchro) resourceSynchroRunner() {
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

		func() {
			s.resourceSynchroLock.Lock()
			defer s.resourceSynchroLock.Unlock()

			s.handlerStopCh = make(chan struct{})
			go func() {
				select {
				case <-s.closer:
				case <-s.stopResourceSynchroCh:
				}

				close(s.handlerStopCh)
			}()

			s.storageResourceSynchros.Range(func(_, value interface{}) bool {
				go value.(*ResourceSynchro).Run(s.handlerStopCh)
				return true
			})
		}()

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

func (s *ClusterSynchro) clusterStatusUpdater() {
	defer close(s.closed)

	for range s.updateStatusCh {
		status := s.genClusterStatus()
		if err := s.ClusterStatusUpdater.UpdateClusterStatus(context.TODO(), s.name, status); err != nil {
			klog.ErrorS(err, "Failed to update cluster ready condition and sync resources status", "cluster", s.name, "conditions", status.Conditions)
		}
	}
}

func (s *ClusterSynchro) updateStatus() {
	select {
	case s.updateStatusCh <- struct{}{}:
	default:
		return
	}
}

func (s *ClusterSynchro) genClusterStatus() *clusterv1alpha2.ClusterStatus {
	status := &clusterv1alpha2.ClusterStatus{
		Version: s.dynamicDiscoveryManager.StorageVersion().GitVersion,
	}

	readyCondition := s.readyCondition.Load().(metav1.Condition)
	if readyCondition.Reason == clusterv1alpha2.ClusterSynchroStopReason {
		synchroCondition := metav1.Condition{
			Type:    clusterv1alpha2.ClusterSynchroCondition,
			Reason:  clusterv1alpha2.ClusterSynchroStopReason,
			Status:  metav1.ConditionFalse,
			Message: "",
		}
		status.Conditions = append(status.Conditions, synchroCondition)
	}
	status.Conditions = append(status.Conditions, readyCondition)

	groupResourceStatuses := s.groupResourceStatus.Load().(*GroupResourceStatus)
	if groupResourceStatuses == nil {
		// syn resources have not been set, not update sync resources
		return status
	}

	statuses := groupResourceStatuses.LoadGroupResourcesStatuses()
	for si, status := range statuses {
		for ri, resource := range status.Resources {
			for vi, cond := range resource.SyncConditions {
				gr := schema.GroupResource{Group: status.Group, Resource: resource.Name}
				storageGVR := cond.StorageGVR(gr)
				if value, ok := s.storageResourceSynchros.Load(storageGVR); ok {
					synchro := value.(*ResourceSynchro)
					if gr != synchro.syncResource.GroupResource() {
						cond.SyncResource = synchro.syncResource.GroupResource().String()
					}
					if cond.Version != synchro.syncResource.Version {
						cond.SyncVersion = synchro.syncResource.Version
					}

					status := synchro.Status()
					cond.Status = status.Status
					cond.Reason = status.Reason
					cond.Message = status.Message
					cond.LastTransitionTime = status.LastTransitionTime
				} else {
					if cond.Status == "" {
						cond.Status = clusterv1alpha2.SyncStatusUnknown
					}
					if cond.Reason == "" {
						cond.Reason = "SynchroNotFound"
					}
					if cond.Message == "" {
						cond.Message = "not found resource synchro"
					}
					cond.LastTransitionTime = metav1.Now().Rfc3339Copy()
				}
				statuses[si].Resources[ri].SyncConditions[vi] = cond
			}
		}
	}
	status.SyncResources = statuses
	return status
}
