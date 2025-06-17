package clustersynchro

import (
	"context"
	"errors"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	genericstorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/tools/cache"
	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/klog/v2"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/runtime/informer"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/features"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/resourcesynchro"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/resourcesynchro/queue"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

type resourceSynchro struct {
	cluster string

	example         runtime.Object
	syncResource    schema.GroupVersionResource
	storageResource schema.GroupVersionResource

	pageSize          int64
	listerWatcher     cache.ListerWatcher
	metricsExtraStore informer.ExtraStore
	metricsWriter     *metricsstore.MetricsWriter
	metricsWrapper    resourcesynchro.MetricsWrapper

	queue   queue.EventQueue
	cache   *informer.ResourceVersionStorage
	rvs     map[string]interface{}
	rvsLock sync.Mutex

	eventSynchro *eventSynchro

	memoryVersion schema.GroupVersion
	storage       storage.ResourceStorage
	convertor     runtime.ObjectConvertor

	status           atomic.Value // clusterv1alpha2.ClusterResourceSyncCondition
	initialListPhase atomic.Bool  // If other phases are added, it can be changed to a more general field.

	startlock sync.Mutex
	stopped   chan struct{}

	// TODO(Iceber): Optimize variable names
	isRunnableForStorage atomic.Bool
	forStorageLock       sync.Mutex
	runnableForStorage   chan struct{}
	stopForStorage       chan struct{}

	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	closer    chan struct{}
	closed    chan struct{}

	// for debug
	runningStage string

	storageMaxRetry int
}

type DefaultResourceSynchroFactory struct{}

var _ resourcesynchro.SynchroFactory = DefaultResourceSynchroFactory{}

func (factory DefaultResourceSynchroFactory) NewResourceSynchro(cluster string, config resourcesynchro.Config) (resourcesynchro.Synchro, error) {
	storageConfig := config.ResourceStorage.GetStorageConfig()
	synchro := &resourceSynchro{
		cluster:         cluster,
		syncResource:    config.GroupVersionResource,
		storageResource: storageConfig.StorageResource,

		pageSize:      config.PageSizeForInformer,
		listerWatcher: config.ListerWatcher,
		rvs:           config.ResourceVersions,

		// all resources saved to the queue are `runtime.Object`
		queue: queue.NewPressureQueue(cache.MetaNamespaceKeyFunc),

		storage:        config.ResourceStorage,
		convertor:      config.ObjectConvertor,
		memoryVersion:  storageConfig.MemoryResource.GroupVersion(),
		metricsWrapper: resourcesynchro.DefaultMetricsWrapperFactory.NewWrapper(cluster, config.GroupVersionResource),

		stopped:            make(chan struct{}),
		runnableForStorage: make(chan struct{}),
		stopForStorage:     make(chan struct{}),

		closer: make(chan struct{}),
		closed: make(chan struct{}),
	}
	synchro.isRunnableForStorage.Store(true)
	close(synchro.runnableForStorage)
	synchro.ctx, synchro.cancel = context.WithCancel(context.Background())

	example := &unstructured.Unstructured{}
	example.SetGroupVersionKind(config.GroupVersionKind())
	synchro.example = example

	if config.MetricsStore != nil {
		synchro.metricsExtraStore = config.MetricsStore
		synchro.metricsWriter = metricsstore.NewMetricsWriter(config.MetricsStore.MetricsStore)
	}

	if config.Event != nil {
		synchro.eventSynchro = newEventSynchro(cluster, synchro,
			informer.NewFilteredListerWatcher(config.Event.ListerWatcher, func(opts *metav1.ListOptions) {
				// https://kubernetes.io/docs/concepts/overview/working-with-objects/field-selectors/#list-of-supported-fields
				field := fields.Set{}
				field["involvedObject.kind"] = config.Kind
				opts.FieldSelector = field.String()
			}), config.Event.ResourceVersions)
	}

	synchro.setStatus(clusterv1alpha2.ResourceSyncStatusPending, "", "")
	return synchro, nil
}

func (synchro *resourceSynchro) Stage() string {
	return synchro.runningStage
}

func (synchro *resourceSynchro) GroupVersionResource() schema.GroupVersionResource {
	return synchro.syncResource
}

func (synchro *resourceSynchro) StoragedGroupVersionResource() schema.GroupVersionResource {
	return synchro.storageResource
}

func (synchro *resourceSynchro) GetMetricsWriter() *metricsstore.MetricsWriter {
	return synchro.metricsWriter
}

func (synchro *resourceSynchro) Run(shutdown <-chan struct{}) {
	defer close(synchro.closed)
	go func() {
		select {
		case <-shutdown:
			synchro.Close()
		case <-synchro.closer:
		}
	}()

	// make `synchro.Start` runable
	close(synchro.stopped)

	if synchro.eventSynchro != nil {
		go func() {
			synchro.eventSynchro.Run(synchro.closer)
		}()
	}

	synchro.runningStage = "running"
	wait.Until(func() {
		synchro.processResources()
	}, time.Second, synchro.closer)
	synchro.runningStage = "processorStop"

	synchro.startlock.Lock()
	synchro.runningStage = "waitStop"
	<-synchro.stopped
	synchro.startlock.Unlock()

	synchro.setStatus(clusterv1alpha2.ResourceSyncStatusStop, "", "")
	synchro.runningStage = "shutdown"

	for _, m := range resourceSynchroMetrics {
		synchro.metricsWrapper.Delete(m.(resourcesynchro.DeletableVecMetrics))
	}
}

func (synchro *resourceSynchro) Close() <-chan struct{} {
	synchro.closeOnce.Do(func() {
		close(synchro.closer)
		synchro.queue.Close()
		synchro.cancel()
	})
	return synchro.closed
}

func (synchro *resourceSynchro) Start(stopCh <-chan struct{}) {
	synchro.startlock.Lock()
	stopped := synchro.stopped // avoid race
	synchro.startlock.Unlock()
	for {
		select {
		case <-stopCh:
			return
		case <-synchro.closer:
			return
		case <-stopped:
		}

		var dorun bool
		func() {
			synchro.startlock.Lock()
			defer synchro.startlock.Unlock()

			select {
			case <-stopCh:
				stopped = nil
				return
			case <-synchro.closer:
				stopped = nil
				return
			default:
			}

			select {
			case <-synchro.stopped:
				dorun = true
				synchro.stopped = make(chan struct{})
			default:
			}

			stopped = synchro.stopped
		}()

		if dorun {
			break
		}
	}

	defer close(synchro.stopped)
	for {
		synchro.forStorageLock.Lock()
		runnableForStorage, stopForStorage := synchro.runnableForStorage, synchro.stopForStorage
		synchro.forStorageLock.Unlock()

		select {
		case <-stopCh:
			synchro.setStatus(clusterv1alpha2.ResourceSyncStatusStop, "Pause", "")
			return
		case <-synchro.closer:
			return
		case <-runnableForStorage:
		}

		select {
		case <-stopCh:
			synchro.setStatus(clusterv1alpha2.ResourceSyncStatusStop, "Pause", "")
			return
		case <-synchro.closer:
			return
		case <-stopForStorage:
			// stopForStorage is closed, storage is not runnable,
			// continue to get `runnableForStorage` and `stopForStorage`
			continue
		default:
		}

		informerStopCh := make(chan struct{})
		go func() {
			select {
			case <-stopCh:
			case <-synchro.closer:
			case <-stopForStorage:
			}
			close(informerStopCh)
		}()

		synchro.rvsLock.Lock()
		if synchro.cache == nil {
			rvs := make(map[string]interface{}, len(synchro.rvs))
			for r, v := range synchro.rvs {
				rvs[r] = v
			}
			synchro.metricsWrapper.Sum(storagedResourcesTotal, float64(len(rvs)))
			synchro.cache = informer.NewResourceVersionStorage()
			synchro.rvsLock.Unlock()

			_ = synchro.cache.Replace(rvs)
		} else {
			synchro.rvsLock.Unlock()
		}

		config := informer.InformerConfig{
			ListerWatcher:     synchro.listerWatcher,
			Storage:           synchro.cache,
			ExampleObject:     synchro.example,
			Handler:           synchro,
			ErrorHandler:      synchro.ErrorHandler,
			ExtraStore:        synchro.metricsExtraStore,
			WatchListPageSize: synchro.pageSize,
		}
		if clusterpediafeature.FeatureGate.Enabled(features.StreamHandlePaginatedListForResourceSync) {
			config.StreamHandleForPaginatedList = true
		}
		if clusterpediafeature.FeatureGate.Enabled(features.ForcePaginatedListForResourceSync) {
			config.ForcePaginatedList = true
		}

		i := informer.NewResourceVersionInformer(synchro.cluster, config)
		go func() {
			synchro.initialListPhase.Store(true)
			if !cache.WaitForCacheSync(informerStopCh, i.HasSynced, func() bool { return !synchro.queue.HasInitialEvent() }) {
				synchro.initialListPhase.Store(false)
				return
			}
			synchro.initialListPhase.Store(false)

			if synchro.eventSynchro != nil {
				synchro.eventSynchro.Start(informerStopCh)
			}
		}()
		i.Run(informerStopCh)

		// TODO(Iceber): Optimize status updates in case of storage exceptions
		if !synchro.isRunnableForStorage.Load() {
			synchro.setStatus(clusterv1alpha2.ResourceSyncStatusStop, "StorageExpection", "")
		}
	}
}

const LastAppliedConfigurationAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

func (synchro *resourceSynchro) pruneObject(obj *unstructured.Unstructured) {
	if clusterpediafeature.FeatureGate.Enabled(features.PruneManagedFields) {
		obj.SetManagedFields(nil)
	}

	if clusterpediafeature.FeatureGate.Enabled(features.PruneLastAppliedConfiguration) {
		annotations := obj.GetAnnotations()
		if _, ok := annotations[LastAppliedConfigurationAnnotation]; ok {
			delete(annotations, LastAppliedConfigurationAnnotation)
			if len(annotations) == 0 {
				annotations = nil
			}
			obj.SetAnnotations(annotations)
		}
	}
}

func (synchro *resourceSynchro) OnAdd(obj interface{}, isInInitialList bool) {
	if !synchro.isRunnableForStorage.Load() {
		return
	}

	// `obj` will not be processed in parallel elsewhere,
	// no deep copy is needed for now.
	//
	// robj := obj.(runtime.Object).DeepCopyObject()

	// Prune object before enqueue.
	//
	// There are many solutions for pruning fields, such as
	// * prunning at the clusterpedia apiserver.
	// * prunning in the storage layer, where neither clustersynchro
	//   nor apiserver are responsible for the pruning process.
	// https://github.com/clusterpedia-io/clusterpedia/issues/4
	synchro.pruneObject(obj.(*unstructured.Unstructured))

	_ = synchro.queue.Add(obj, isInInitialList)
}

func (synchro *resourceSynchro) OnUpdate(_, obj interface{}, isInInitialList bool) {
	if !synchro.isRunnableForStorage.Load() {
		return
	}

	// `obj` will not be processed in parallel elsewhere,
	// no deep copy is needed for now.
	//
	// robj := obj.(runtime.Object).DeepCopyObject()

	// https://github.com/clusterpedia-io/clusterpedia/issues/4
	synchro.pruneObject(obj.(*unstructured.Unstructured))
	_ = synchro.queue.Update(obj, isInInitialList)
}

func (synchro *resourceSynchro) OnDelete(obj interface{}, isInInitialList bool) {
	if !synchro.isRunnableForStorage.Load() {
		return
	}
	if o, ok := obj.(*unstructured.Unstructured); ok {
		synchro.pruneObject(o)
	}

	obj, err := synchro.storage.ConvertDeletedObject(obj)
	if err != nil {
		return
	}
	_ = synchro.queue.Delete(obj, isInInitialList)
}

func (synchro *resourceSynchro) OnSync(obj interface{}) {}

func (synchro *resourceSynchro) processResources() {
	for {
		select {
		case <-synchro.closer:
			return
		default:
		}

		event, err := synchro.queue.Pop()
		if err != nil {
			if err == queue.ErrQueueClosed {
				return
			}

			klog.Error(err)
			continue
		}

		synchro.handleResourceEvent(event)
	}
}

func (synchro *resourceSynchro) handleResourceEvent(event *queue.Event) {
	defer func() { _ = synchro.queue.Done(event) }()

	obj, ok := event.Object.(runtime.Object)
	if !ok {
		return
	}
	key, _ := cache.MetaNamespaceKeyFunc(obj)

	var callback func(obj runtime.Object)
	var handler func(ctx context.Context, obj runtime.Object) error
	if event.Action != queue.Deleted {
		var err error
		if obj, err = synchro.convertToStorageVersion(obj); err != nil {
			klog.ErrorS(err, "Failed to convert resource", "cluster", synchro.cluster,
				"action", event.Action, "resource", synchro.storageResource, "key", key)
			return
		}
		utils.InjectClusterName(obj, synchro.cluster)

		var metric compbasemetrics.CounterMetric
		switch event.Action {
		case queue.Added:
			handler = synchro.createOrUpdateResource
			metric = synchro.metricsWrapper.Counter(resourceAddedCounter)
		case queue.Updated:
			handler = synchro.updateOrCreateResource
			metric = synchro.metricsWrapper.Counter(resourceUpdatedCounter)
		}
		callback = func(obj runtime.Object) {
			metric.Inc()
			metaobj, _ := meta.Accessor(obj)
			synchro.rvsLock.Lock()
			synchro.rvs[key] = metaobj.GetResourceVersion()

			synchro.metricsWrapper.Sum(storagedResourcesTotal, float64(len(synchro.rvs)))
			synchro.rvsLock.Unlock()
		}
	} else {
		handler, callback = synchro.deleteResource, func(_ runtime.Object) {
			synchro.rvsLock.Lock()
			delete(synchro.rvs, key)
			synchro.metricsWrapper.Sum(storagedResourcesTotal, float64(len(synchro.rvs)))
			synchro.rvsLock.Unlock()
			synchro.metricsWrapper.Counter(resourceDeletedCounter).Inc()
		}
	}

	// TODO(Iceber): put the event back into the queue to retry?
	for i := 0; ; i++ {
		now := time.Now()
		ctx, cancel := context.WithTimeout(synchro.ctx, 30*time.Second)
		err := handler(ctx, obj)
		cancel()
		if err == nil {
			callback(obj)

			if i != 0 && i > synchro.storageMaxRetry {
				synchro.storageMaxRetry = i
				synchro.metricsWrapper.Max(resourceMaxRetryGauge, float64(i))
			}

			if !synchro.isRunnableForStorage.Load() && synchro.queue.Len() == 0 {
				// Start the informer after processing the data in the queue to ensure that storage is up and running for a period of time.
				synchro.setRunnableForStorage()
			}
			synchro.metricsWrapper.Historgram(resourceStorageDuration).Observe(time.Since(now).Seconds())
			return
		}

		if errors.Is(err, context.Canceled) {
			return
		}

		synchro.metricsWrapper.Counter(resourceFailedCounter).Inc()
		if !storage.IsRecoverableException(err) {
			synchro.metricsWrapper.Counter(resourceDroppedCounter).Inc()
			klog.ErrorS(err, "Failed to storage resource", "cluster", synchro.cluster,
				"action", event.Action, "resource", synchro.storageResource, "key", key)

			if !synchro.isRunnableForStorage.Load() && synchro.queue.Len() == 0 {
				// if the storage returns an error on stopForStorage that cannot be recovered
				// and the len(queue) is empty, start the informer
				synchro.setRunnableForStorage()
			}
			return
		}

		// Store component exceptions, control informer start/stop, and retry sync at regular intervals

		// After five retries, if the data in the queue is greater than 5,
		// keep only 5 items of data in the queue and stop informer to avoid a large accumulation of resources in memory
		var retainInQueue = 5
		if i >= 5 && synchro.queue.Len() > retainInQueue {
			if synchro.isRunnableForStorage.Load() {
				synchro.setStopForStorage()
			}
			synchro.queue.DiscardAndRetain(retainInQueue)

			// If the data in the queue is discarded,
			// the data in the cache will be inconsistent with the data in the `rvs`,
			// delete the cache, and trigger the reinitialization of the cache when the informer is started.
			synchro.rvsLock.Lock()
			synchro.cache = nil
			synchro.rvsLock.Unlock()
		}

		//	klog.ErrorS(err, "will retry sync storage resource", "num", i, "cluster", synchro.cluster,
		//		"action", event.Action, "resource", synchro.storageResource, "key", key)
		time.Sleep(2 * time.Second)
	}
}

func (synchro *resourceSynchro) setRunnableForStorage() {
	synchro.isRunnableForStorage.Store(true)

	synchro.forStorageLock.Lock()
	defer synchro.forStorageLock.Unlock()

	select {
	case <-synchro.runnableForStorage:
	default:
		close(synchro.runnableForStorage)
	}
	select {
	case <-synchro.stopForStorage:
		synchro.stopForStorage = make(chan struct{})
	default:
	}
}

func (synchro *resourceSynchro) setStopForStorage() {
	synchro.isRunnableForStorage.Store(false)

	synchro.forStorageLock.Lock()
	defer synchro.forStorageLock.Unlock()

	select {
	case <-synchro.runnableForStorage:
		synchro.runnableForStorage = make(chan struct{})
	default:
	}
	select {
	case <-synchro.stopForStorage:
	default:
		close(synchro.stopForStorage)
	}
}

func (synchro *resourceSynchro) convertToStorageVersion(obj runtime.Object) (runtime.Object, error) {
	if synchro.syncResource == synchro.storageResource || synchro.convertor == nil {
		return obj, nil
	}

	// convert to hub version
	obj, err := synchro.convertor.ConvertToVersion(obj, synchro.memoryVersion)
	if err != nil {
		return nil, err
	}

	if synchro.memoryVersion == synchro.storageResource.GroupVersion() {
		return obj, nil
	}

	// convert to storage version
	obj, err = synchro.convertor.ConvertToVersion(obj, synchro.storageResource.GroupVersion())
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (synchro *resourceSynchro) createOrUpdateResource(ctx context.Context, obj runtime.Object) error {
	err := synchro.storage.Create(ctx, synchro.cluster, obj)
	if genericstorage.IsExist(err) {
		return synchro.storage.Update(ctx, synchro.cluster, obj)
	}
	return err
}

func (synchro *resourceSynchro) updateOrCreateResource(ctx context.Context, obj runtime.Object) error {
	err := synchro.storage.Update(ctx, synchro.cluster, obj)
	if genericstorage.IsNotFound(err) {
		return synchro.storage.Create(ctx, synchro.cluster, obj)
	}
	return err
}

func (synchro *resourceSynchro) deleteResource(ctx context.Context, obj runtime.Object) error {
	return synchro.storage.Delete(ctx, synchro.cluster, obj)
}

func (synchro *resourceSynchro) setStatus(status string, reason, message string) {
	synchro.status.Store(clusterv1alpha2.ClusterResourceSyncCondition{
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	})
}

func (synchro *resourceSynchro) Status() clusterv1alpha2.ClusterResourceSyncCondition {
	s := synchro.status.Load().(clusterv1alpha2.ClusterResourceSyncCondition)
	switch s.Status {
	case clusterv1alpha2.ResourceSyncStatusPending, clusterv1alpha2.ResourceSyncStatusSyncing:
		s.InitialListPhase = synchro.initialListPhase.Load()
	}
	return s
}

func (synchro *resourceSynchro) ErrorHandler(r *informer.Reflector, err error) {
	if err != nil {
		// TODO(iceber): Use `k8s.io/apimachinery/pkg/api/errors` to resolve the error type and update it to `status.Reason`
		synchro.setStatus(clusterv1alpha2.ResourceSyncStatusError, "ResourceWatchFailed", err.Error())
		informer.DefaultWatchErrorHandler(r, err)
		return
	}

	// `reflector` sets a default timeout when watching,
	// then when re-watching the error handler is called again and the `err` is nil.
	// if the current status is Syncing, then the status is not updated to avoid triggering a cluster status update
	status := synchro.status.Load().(clusterv1alpha2.ClusterResourceSyncCondition)
	if status.Status != clusterv1alpha2.ResourceSyncStatusSyncing {
		synchro.setStatus(clusterv1alpha2.ResourceSyncStatusSyncing, "", "")
	}
}

type eventSynchro struct {
	ctx    context.Context
	closer <-chan struct{}

	cluster       string
	synchro       *resourceSynchro
	listerWatcher cache.ListerWatcher

	queue   queue.EventQueue
	rvs     map[string]interface{}
	rvsLock sync.Mutex
	cache   *informer.ResourceVersionStorage
}

func newEventSynchro(cluster string, synchro *resourceSynchro, lw cache.ListerWatcher, rvs map[string]interface{}) *eventSynchro {
	return &eventSynchro{
		cluster:       cluster,
		listerWatcher: lw,
		synchro:       synchro,
		rvs:           rvs,
		queue:         queue.NewPressureQueue(cache.MetaNamespaceKeyFunc),
	}
}

func (synchro *eventSynchro) Run(closer <-chan struct{}) {
	synchro.closer = closer

	wait.Until(func() {
		synchro.processResources()
	}, time.Second, closer)
	synchro.queue.Close()
}

func (synchro *eventSynchro) Start(stop <-chan struct{}) {
	synchro.rvsLock.Lock()
	rvs := maps.Clone(synchro.rvs)
	synchro.rvsLock.Unlock()

	synchro.cache = informer.NewResourceVersionStorage()
	_ = synchro.cache.Replace(rvs)

	config := informer.InformerConfig{
		ListerWatcher: synchro.listerWatcher,
		Storage:       synchro.cache,
		ExampleObject: &corev1.Event{},
		Handler:       synchro,
	}
	informer.NewResourceVersionInformer(synchro.cluster, config).Run(stop)
}

func (synchro *eventSynchro) isOrphanEvent(event *corev1.Event) bool {
	key := cache.NewObjectName(event.InvolvedObject.Namespace, event.InvolvedObject.Name).String()
	synchro.synchro.rvsLock.Lock()
	_, ok := synchro.synchro.rvs[key]
	synchro.synchro.rvsLock.Unlock()
	return !ok
}

func (synchro *eventSynchro) OnAdd(obj interface{}, _ bool) {
	if synchro.isOrphanEvent(obj.(*corev1.Event)) {
		// TODO(Iceber): cache orphan events
		return
	}
	_ = synchro.queue.Add(obj, false)
}

func (synchro *eventSynchro) OnUpdate(_, obj interface{}, _ bool) {
	if synchro.isOrphanEvent(obj.(*corev1.Event)) {
		// TODO(Iceber): cache orphan events
		return
	}
	_ = synchro.queue.Update(obj, false)
}

func (synchro *eventSynchro) OnDelete(obj interface{}, _ bool) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	synchro.rvsLock.Lock()
	delete(synchro.rvs, key)
	synchro.rvsLock.Unlock()
}

func (*eventSynchro) OnSync(obj interface{}) {
	// ignore deletion event
}

func (synchro *eventSynchro) processResources() {
	for {
		select {
		case <-synchro.closer:
			return
		default:
		}

		event, err := synchro.queue.Pop()
		if err != nil {
			if err == queue.ErrQueueClosed {
				return
			}
			klog.Error(err)
			continue
		}

		synchro.handleResourceEvent(event)
	}
}

func (synchro *eventSynchro) handleResourceEvent(event *queue.Event) {
	defer func() { _ = synchro.queue.Done(event) }()

	obj := event.Object.(*corev1.Event)
	obj.SetManagedFields(nil)
	err := synchro.synchro.storage.RecordEvent(synchro.ctx, synchro.cluster, obj)
	if err != nil {
		klog.ErrorS(err, "Failed to storage event", "cluster", synchro.cluster)
		return
	}

	key, _ := cache.MetaNamespaceKeyFunc(obj)

	synchro.rvsLock.Lock()
	synchro.rvs[key] = obj.GetResourceVersion()
	synchro.rvsLock.Unlock()
}
