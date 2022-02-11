package clustersynchro

import (
	"context"
	"errors"
	"fmt"
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

	clustersv1alpha2 "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusters/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
)

type ClusterStatusUpdater interface {
	UpdateClusterStatus(ctx context.Context, name string, status *clustersv1alpha2.ClusterStatus) error
}

type ClusterSynchro struct {
	name string

	RESTConfig           *rest.Config
	ClusterStatusUpdater ClusterStatusUpdater

	restmapper           meta.RESTMapper
	clusterclient        kubernetes.Interface
	listerWatcherFactory informer.DynamicListerWatcherFactory

	storage               storage.StorageFactory
	resourceStorageConfig *storageconfig.StorageConfigFactory

	closeOnce sync.Once
	closer    chan struct{}
	closed    chan struct{}

	status chan struct{}

	runResourceSynchroCh  chan struct{}
	stopResourceSynchroCh chan struct{}

	waitGroup                wait.Group
	resourceSynchroWaitGroup wait.Group

	resourcelock  sync.RWMutex
	handlerStopCh chan struct{}
	// Key is the storage resource.
	// Sometimes the synchronized resource and the storage resource are different
	resourceVersionCaches map[schema.GroupVersionResource]*informer.ResourceVersionStorage
	resourceSynchros      atomic.Value // map[schema.GroupVersionResource]*ResourceSynchro

	sortedGroupResources atomic.Value // []schema.GroupResource
	resourceStatuses     atomic.Value // map[schema.GroupResource]*clustersv1alpha2.ClusterResourceStatus

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

	version, err := clusterclient.Discovery().ServerVersion()
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

		restmapper:            mapper,
		clusterclient:         clusterclient,
		listerWatcherFactory:  informer.NewDynamicListWatcherFactory(dynamaicclient),
		resourceStorageConfig: storageconfig.NewStorageConfigFactory(),

		status: make(chan struct{}, 1),
		closer: make(chan struct{}),
		closed: make(chan struct{}),

		runResourceSynchroCh:  make(chan struct{}),
		stopResourceSynchroCh: make(chan struct{}),

		resourceVersionCaches: make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage),
	}
	synchro.version.Store(*version)
	synchro.sortedGroupResources.Store([]schema.GroupResource{})
	synchro.resourceStatuses.Store(map[schema.GroupResource]*clustersv1alpha2.ClusterResourceStatus{})

	synchro.resourceSynchros.Store(map[schema.GroupVersionResource]*ResourceSynchro{})

	condition := metav1.Condition{
		Type:               clustersv1alpha2.ClusterConditionReady,
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

	resourceVersionCaches := make(map[schema.GroupVersionResource]*informer.ResourceVersionStorage, len(resourceversions))
	for gvr, rvs := range resourceversions {
		cache := informer.NewResourceVersionStorage(cache.DeletionHandlingMetaNamespaceKeyFunc)
		cache.Replace(rvs)
		resourceVersionCaches[gvr] = cache
	}

	s.resourceVersionCaches = resourceVersionCaches
}

type syncConfig struct {
	kind            string
	syncResource    schema.GroupVersionResource
	storageResource schema.GroupVersionResource
	convertor       runtime.ObjectConvertor
	storageConfig   *storage.ResourceStorageConfig
}

func (s *ClusterSynchro) SetResources(syncResources []clustersv1alpha2.ClusterGroupResources) {
	var (
		// syncConfigs key is resource's storage gvr
		syncConfigs = map[schema.GroupVersionResource]*syncConfig{}

		sortedGroupResources = []schema.GroupResource{}
		resourceStatuses     = map[schema.GroupResource]*clustersv1alpha2.ClusterResourceStatus{}
	)

	for _, groupResources := range syncResources {
		for _, resource := range groupResources.Resources {
			gr := schema.GroupResource{Group: groupResources.Group, Resource: resource}
			supportedGVKs, err := s.restmapper.KindsFor(gr.WithVersion(""))
			if err != nil {
				klog.ErrorS(fmt.Errorf("Cluster not supported resource: %v", err), "Skip resource sync", "cluster", s.name, "resource", gr)
				continue
			}

			syncVersions, isLegacyResource, err := negotiateSyncVersions(groupResources.Versions, supportedGVKs)
			if err != nil {
				klog.InfoS("Skip resource sync", "cluster", s.name, "resource", gr, "reason", err)
				continue
			}

			mapper, err := s.restmapper.RESTMapping(supportedGVKs[0].GroupKind(), supportedGVKs[0].Version)
			if err != nil {
				klog.ErrorS(err, "Skip resource sync", "cluster", s.name, "resource", gr)
				continue
			}

			info := &clustersv1alpha2.ClusterResourceStatus{
				Kind:       mapper.GroupVersionKind.Kind,
				Resource:   gr.Resource,
				Namespaced: mapper.Scope.Name() == meta.RESTScopeNameNamespace,
			}

			for _, version := range syncVersions {
				syncResource := gr.WithVersion(version)
				storageConfig, err := s.resourceStorageConfig.NewConfig(syncResource)
				if err != nil {
					// TODO(iceber): set storage error ?
					klog.ErrorS(err, "Failed to create resource storage config", "cluster", s.name, "resource", syncResource)
					continue
				}

				storageResource := storageConfig.StorageGroupResource.WithVersion(storageConfig.StorageVersion.Version)
				if _, ok := syncConfigs[storageResource]; !ok {
					config := &syncConfig{
						kind:            info.Kind,
						syncResource:    syncResource,
						storageResource: storageResource,
						storageConfig:   storageConfig,
					}

					if syncResource != storageResource {
						if isLegacyResource {
							config.convertor = resourcescheme.LegacyResourceScheme
						} else {
							config.convertor = resourcescheme.CustomResourceScheme
						}
					}
					syncConfigs[storageResource] = config
				}

				syncCondition := clustersv1alpha2.ClusterResourceSyncCondition{
					Version:        version,
					StorageVersion: storageConfig.StorageVersion.Version,
					Status:         clustersv1alpha2.SyncStatusPending,
					Reason:         "SynchroCreating",
				}
				if gr != storageConfig.StorageGroupResource {
					storageResource := storageConfig.StorageGroupResource.String()
					syncCondition.StorageResource = &storageResource
				}
				info.SyncConditions = append(info.SyncConditions, syncCondition)
			}

			resourceStatuses[gr] = info
			sortedGroupResources = append(sortedGroupResources, gr)
		}
	}

	s.resourcelock.Lock()
	defer s.resourcelock.Unlock()
	select {
	case <-s.closer:
		return
	default:
	}

	s.sortedGroupResources.Store(sortedGroupResources)
	s.resourceStatuses.Store(resourceStatuses)

	// filter deleted resources
	deleted := map[schema.GroupVersionResource]struct{}{}
	for gvr := range s.resourceVersionCaches {
		if _, ok := syncConfigs[gvr]; !ok {
			deleted[gvr] = struct{}{}
		}
	}

	synchros := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)

	// remove deleted resource synchro
	for gvr := range deleted {
		if handler, ok := synchros[gvr]; ok {
			handler.Close()

			// ensure that no more data is synchronized to the storage.
			<-handler.Closed()
			delete(synchros, gvr)
		}

		if err := s.storage.CleanClusterResource(context.TODO(), s.name, gvr); err != nil {
			klog.ErrorS(err, "Failed to clean cluster resource", "cluster", s.name, "resource", gvr)
			// update resource sync status
			continue
		}

		delete(s.resourceVersionCaches, gvr)
	}

	for gvr, config := range syncConfigs {
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

		syncKind := config.syncResource.GroupVersion().WithKind(config.kind)
		synchro := newResourceSynchro(s.name, syncKind,
			s.listerWatcherFactory.ForResource(metav1.NamespaceAll, config.syncResource),
			resourceVersionCache,
			config.convertor,
			resourceStorage,
		)
		s.resourceSynchroWaitGroup.StartWithChannel(s.closer, synchro.runStorager)

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

func (s *ClusterSynchro) Run(shutdown <-chan struct{}) {
	s.waitGroup.Start(s.Monitor)
	s.waitGroup.Start(s.resourceSynchroRunner)
	go s.clusterStatusUpdater()

	select {
	case <-shutdown:
		s.Shutdown(true, false)
	case <-s.closer:
		// clustersynchro.Shutdown has been called, wait for closed.
		<-s.closed
	}
}

func (s *ClusterSynchro) Shutdown(updateReadyCondition, waitResourceSynchro bool) {
	// ensure that we cannot call SetResource after closing `synchro.closer`
	s.resourcelock.Lock()
	s.closeOnce.Do(func() {
		close(s.closer)
	})
	s.resourcelock.Unlock()

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
			Type:               clustersv1alpha2.ClusterConditionReady,
			Status:             metav1.ConditionUnknown,
			Reason:             "ClusterSynchroStop",
			Message:            message,
			LastTransitionTime: metav1.Now(),
		}
		s.readyCondition.Store(condition)

		s.updateStatus()
	}

	close(s.status)
	<-s.closed
}

func (s *ClusterSynchro) genClusterStatus() *clustersv1alpha2.ClusterStatus {
	sortedGroupResources := s.sortedGroupResources.Load().([]schema.GroupResource)
	resourceStatuses := s.resourceStatuses.Load().(map[schema.GroupResource]*clustersv1alpha2.ClusterResourceStatus)
	synchros := s.resourceSynchros.Load().(map[schema.GroupVersionResource]*ResourceSynchro)

	groups := make(map[string]*clustersv1alpha2.ClusterGroupResourcesStatus)
	groupStatuses := make([]clustersv1alpha2.ClusterGroupResourcesStatus, 0)
	for _, gr := range sortedGroupResources {
		resourceStatus, ok := resourceStatuses[gr]
		if !ok {
			continue
		}

		groupStatus, ok := groups[gr.Group]
		if !ok {
			groupStatuses = append(groupStatuses, clustersv1alpha2.ClusterGroupResourcesStatus{})
			groupStatus = &groupStatuses[len(groupStatuses)-1]
			groups[gr.Group] = groupStatus
		}

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
					cond.Status = clustersv1alpha2.SyncStatusPending
				}
				if cond.Reason == "" {
					cond.Reason = "SynchroNotFound"
				}
				if cond.Message == "" {
					cond.Message = "not found resource synchro"
				}
				cond.LastTransitionTime = metav1.Now().Rfc3339Copy()
			}

			// TODO(iceber): if synchro is closed, set cond.Status == Stop

			resourceStatus.SyncConditions[i] = cond
		}

		groupStatus.Group = gr.Group
		groupStatus.Resources = append(groupStatus.Resources, *resourceStatus)
	}

	version := s.version.Load().(version.Info).GitVersion
	readyCondition := s.readyCondition.Load().(metav1.Condition)
	return &clustersv1alpha2.ClusterStatus{
		Version:       version,
		Conditions:    []metav1.Condition{readyCondition},
		SyncResources: groupStatuses,
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
	defer close(s.closed)

	for range s.status {
		status := s.genClusterStatus()
		if err := s.ClusterStatusUpdater.UpdateClusterStatus(context.TODO(), s.name, status); err != nil {
			klog.ErrorS(err, "Failed to update cluster status", "cluster", s.name, status.Conditions[0].Reason)
		}
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
				Type:               clustersv1alpha2.ClusterConditionReady,
				Status:             metav1.ConditionTrue,
				Reason:             "Healthy",
				LastTransitionTime: metav1.Now().Rfc3339Copy(),
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
		Type:   clustersv1alpha2.ClusterConditionReady,
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
		condition.LastTransitionTime = metav1.Now().Rfc3339Copy()
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

func negotiateSyncVersions(syncVersions []string, supportedGVKs []schema.GroupVersionKind) ([]string, bool, error) {
	if len(supportedGVKs) == 0 {
		return nil, false, errors.New("The supported versions are empty, the resource is not supported")
	}

	knowns := resourcescheme.LegacyResourceScheme.VersionsForGroupKind(supportedGVKs[0].GroupKind())
	if len(knowns) != 0 {
		var preferredVersion schema.GroupVersion
	Loop:
		for _, gvk := range supportedGVKs {
			for _, gv := range knowns {
				if gvk.GroupVersion() == gv {
					preferredVersion = gv
					break Loop
				}
			}
		}

		if preferredVersion.Empty() {
			// For legacy resources, only known version are synchronized,
			// and only one version is guaranteed to be synchronized and saved.
			return nil, true, errors.New("The supported versions do not contain any known versions")
		}
		return []string{preferredVersion.Version}, true, nil
	}

	// For custom resources, if the version to be synchronized is not specified,
	// then the first three versions available from the cluster are used
	if len(syncVersions) == 0 {
		for _, gvk := range supportedGVKs {
			syncVersions = append(syncVersions, gvk.Version)
		}
		if len(syncVersions) > 3 {
			syncVersions = syncVersions[:3]
		}
		return syncVersions, false, nil
	}

	// Handles custom resources that specify sync versions
	var filtered []string
	for _, version := range syncVersions {
		for _, gvk := range supportedGVKs {
			if gvk.Version == version {
				filtered = append(filtered, version)
			}
		}
	}

	if len(filtered) == 0 {
		return nil, false, errors.New("The supported versions do not contain any specified sync version")
	}
	return filtered, false, nil
}
