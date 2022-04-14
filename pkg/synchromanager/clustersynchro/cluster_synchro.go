package clustersynchro

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
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

	waitGroup                wait.Group
	resourceSynchroWaitGroup wait.Group

	resourceSynchroLock sync.RWMutex
	handlerStopCh       chan struct{}
	// Key is the storage resource.
	// Sometimes the synchronized resource and the storage resource are different
	storageResourceVersionCaches map[schema.GroupVersionResource]*informer.ResourceVersionStorage
	storageResourceSynchros      sync.Map

	syncResources            atomic.Value // []clusterv1alpha2.ClusterGroupResources
	setSyncResourcesCh       chan struct{}
	customResourceController *CustomResourceController

	resourceNegotiator  *ResourceNegotiator
	groupResourceStatus atomic.Value // *GroupResourceStatus

	version        atomic.Value // version.Info
	readyCondition atomic.Value // metav1.Condition
}

type ClusterStatusUpdater interface {
	UpdateClusterStatus(ctx context.Context, name string, status *clusterv1alpha2.ClusterStatus) error
}

func New(name string, config *rest.Config, storage storage.StorageFactory, updater ClusterStatusUpdater) (*ClusterSynchro, error) {
	clusterclient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		return nil, err
	}

	crdGVRs, err := mapper.ResourcesFor(schema.GroupVersionResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"})
	if err != nil {
		return nil, err
	}
	if len(crdGVRs) == 0 {
		return nil, errors.New("not match crd version")
	}

	version, err := clusterclient.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	listWatchFactory, err := informer.NewDynamicListerWatcherFactory(config)
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

		clusterclient:        clusterclient,
		listerWatcherFactory: listWatchFactory,

		closer: make(chan struct{}),
		closed: make(chan struct{}),

		updateStatusCh:        make(chan struct{}, 1),
		runResourceSynchroCh:  make(chan struct{}),
		stopResourceSynchroCh: make(chan struct{}),

		storageResourceVersionCaches: make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage),
	}
	synchro.version.Store(*version)

	customResourceController, err := NewCustomResourceController(name, config, crdGVRs[0].Version)
	if err != nil {
		return nil, err
	}
	synchro.customResourceController = customResourceController

	synchro.resourceNegotiator = &ResourceNegotiator{
		name:                     name,
		restmapper:               mapper,
		resourceStorageConfig:    storageconfig.NewStorageConfigFactory(),
		customResourceController: customResourceController,
	}
	synchro.groupResourceStatus.Store(NewGroupResourceStatus())

	synchro.syncResources.Store([]clusterv1alpha2.ClusterGroupResources(nil))
	synchro.setSyncResourcesCh = make(chan struct{}, 1)

	condition := metav1.Condition{
		Type:               clusterv1alpha2.ClusterConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Pending",
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
	go s.customResourceController.Run(s.closer)
	go func() {
		if cache.WaitForNamedCacheSync(s.name+"-CustomResourceController", s.closer, s.customResourceController.HasSynced) {
			s.customResourceController.SetResourceMutationHandler(s.resetSyncResources)
			s.resetSyncResources()
		}
	}()

	s.waitGroup.Start(s.monitor)
	s.waitGroup.Start(s.resourceSynchroRunner)
	go s.clusterStatusUpdater()
	go s.syncResourcesSetter()

	select {
	case <-shutdown:
		s.Shutdown(true, false)
	case <-s.closer:
	}
	<-s.closed
}

func (s *ClusterSynchro) Shutdown(updateReadyCondition, waitResourceSynchro bool) {
	s.closeOnce.Do(func() {
		close(s.closer)
	})

	if waitResourceSynchro {
		// wait for all resource synchros to shutdown,
		// to ensure that no more data is synchronized to the storage
		s.resourceSynchroWaitGroup.Wait()
	}

	s.waitGroup.Wait()

	if updateReadyCondition {
		var message string
		lastReadyCondition := s.readyCondition.Load().(metav1.Condition)
		if lastReadyCondition.Status == metav1.ConditionFalse {
			message = fmt.Sprintf("Last Condition Reason: %s, Message: %s", lastReadyCondition.Reason, lastReadyCondition.Message)
		}
		condition := metav1.Condition{
			Type:               clusterv1alpha2.ClusterConditionReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "ClusterSynchroStop",
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
	s.customResourceController.SetSyncAllCustomResources(syncAllCustomResources)
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

	// TODO: HasSynced needs to lock the informer.queue, replace it with a more lightweight way
	if !s.customResourceController.HasSynced() {
		klog.InfoS("custom resource controller is not synced", "cluster", s.name)
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
			s.resourceSynchroWaitGroup.StartWithChannel(s.closer, synchro.runStorager)
			s.storageResourceSynchros.Store(storageGVR, synchro)

			// After the synchronizer is successfully created,
			// clean up the reasons and message initialized in the sync condition
			updateSyncConditions(storageGVR, clusterv1alpha2.SyncStatusUnknown, "", "")

			if s.handlerStopCh != nil {
				select {
				case <-s.handlerStopCh:
				default:
					go synchro.Run(s.handlerStopCh)
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
			klog.ErrorS(err, "Failed to update cluster status", "cluster", s.name, status.Conditions[0].Reason)
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
	groupResourceStatuses := s.groupResourceStatus.Load().(*GroupResourceStatus)
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

	version := s.version.Load().(version.Info).GitVersion
	readyCondition := s.readyCondition.Load().(metav1.Condition)
	return &clusterv1alpha2.ClusterStatus{
		Version:       version,
		Conditions:    []metav1.Condition{readyCondition},
		SyncResources: statuses,
	}
}
