package clustersynchro

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	genericstorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/queue"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/features"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

type ResourceSynchro struct {
	cluster string

	example         runtime.Object
	syncResource    schema.GroupVersionResource
	storageResource schema.GroupVersionResource

	workerNum     int
	queue         queue.EventQueue
	listerWatcher cache.ListerWatcher
	cache         *informer.ResourceVersionStorage

	memoryVersion schema.GroupVersion
	storage       storage.ResourceStorage
	convertor     runtime.ObjectConvertor

	status atomic.Value // clusterv1alpha2.ClusterResourceSyncCondition

	startlock sync.Mutex
	stoped    chan struct{}

	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	closer    chan struct{}
	closed    chan struct{}
}

func newResourceSynchro(cluster string, syncResource schema.GroupVersionResource, kind string, lw cache.ListerWatcher, rvcache *informer.ResourceVersionStorage,
	convertor runtime.ObjectConvertor, storage storage.ResourceStorage,
) *ResourceSynchro {
	storageConfig := storage.GetStorageConfig()
	ctx, cancel := context.WithCancel(context.Background())
	synchro := &ResourceSynchro{
		cluster:         cluster,
		syncResource:    syncResource,
		storageResource: storageConfig.StorageGroupResource.WithVersion(storageConfig.StorageVersion.Version),
		workerNum:       1,
		listerWatcher:   lw,
		cache:           rvcache,
		queue:           queue.NewPressureQueue(cache.DeletionHandlingMetaNamespaceKeyFunc),

		storage:       storage,
		convertor:     convertor,
		memoryVersion: storageConfig.MemoryVersion,

		ctx:    ctx,
		cancel: cancel,
		closer: make(chan struct{}),
		closed: make(chan struct{}),

		stoped: make(chan struct{}),
	}

	example := &unstructured.Unstructured{}
	example.SetGroupVersionKind(syncResource.GroupVersion().WithKind(kind))
	synchro.example = example

	status := clusterv1alpha2.ClusterResourceSyncCondition{
		Status:             clusterv1alpha2.SyncStatusPending,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)
	return synchro
}

func (synchro *ResourceSynchro) Run(shutdown <-chan struct{}) {
	go func() {
		select {
		case <-shutdown:
			synchro.Close()
		case <-synchro.closer:
		}
	}()

	// make `synchro.Start` runable
	close(synchro.stoped)

	var waitGroup sync.WaitGroup
	for i := 0; i < synchro.workerNum; i++ {
		waitGroup.Add(1)

		go wait.Until(func() {
			defer waitGroup.Done()
			synchro.processResources()
		}, time.Second, synchro.closer)
	}
	waitGroup.Wait()

	synchro.startlock.Lock()
	<-synchro.stoped
	synchro.startlock.Unlock()

	status := clusterv1alpha2.ClusterResourceSyncCondition{
		Status:             clusterv1alpha2.SyncStatusStop,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)

	close(synchro.closed)
}

func (synchro *ResourceSynchro) Close() <-chan struct{} {
	synchro.closeOnce.Do(func() {
		close(synchro.closer)
		synchro.queue.Close()
		synchro.cancel()
	})
	return synchro.closed
}

func (synchro *ResourceSynchro) Start(stopCh <-chan struct{}) {
	synchro.startlock.Lock()
	stoped := synchro.stoped // avoid race
	synchro.startlock.Unlock()
	for {
		select {
		case <-stopCh:
			return
		case <-synchro.closer:
			return
		case <-stoped:
		}

		var dorun bool
		func() {
			synchro.startlock.Lock()
			defer synchro.startlock.Unlock()

			select {
			case <-stopCh:
				stoped = nil
				return
			case <-synchro.closer:
				stoped = nil
				return
			default:
			}

			select {
			case <-synchro.stoped:
				dorun = true
				synchro.stoped = make(chan struct{})
			default:
			}

			stoped = synchro.stoped
		}()

		if dorun {
			break
		}
	}

	defer close(synchro.stoped)
	select {
	case <-stopCh:
		return
	case <-synchro.closer:
		return
	default:
	}

	informerStopCh := make(chan struct{})
	go func() {
		select {
		case <-stopCh:
		case <-synchro.closer:
		}
		close(informerStopCh)
	}()

	status := clusterv1alpha2.ClusterResourceSyncCondition{
		Status:             clusterv1alpha2.SyncStatusStart,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)

	informer.NewResourceVersionInformer(
		synchro.cluster,
		synchro.listerWatcher,
		synchro.cache,
		synchro.example,
		synchro,
	).Run(informerStopCh)

	status = clusterv1alpha2.ClusterResourceSyncCondition{
		Status:             clusterv1alpha2.SyncStatusStop,
		Reason:             "Pause",
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)
}

const LastAppliedConfigurationAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

func (synchro *ResourceSynchro) pruneObject(obj *unstructured.Unstructured) {
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

func (synchro *ResourceSynchro) OnAdd(obj interface{}) {
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

	_ = synchro.queue.Add(obj)
}

func (synchro *ResourceSynchro) OnUpdate(_, obj interface{}) {
	// `obj` will not be processed in parallel elsewhere,
	// no deep copy is needed for now.
	//
	// robj := obj.(runtime.Object).DeepCopyObject()

	// https://github.com/clusterpedia-io/clusterpedia/issues/4
	synchro.pruneObject(obj.(*unstructured.Unstructured))
	_ = synchro.queue.Update(obj)
}

func (synchro *ResourceSynchro) OnDelete(obj interface{}) {
	_ = synchro.queue.Delete(obj)
}

func (synchro *ResourceSynchro) OnSync(obj interface{}) {
}

func (synchro *ResourceSynchro) processResources() {
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

		status := clusterv1alpha2.ClusterResourceSyncCondition{}
		// before handle the reource event, set the status Syncing to the synchro.
		if synchro.Status().Status != clusterv1alpha2.SyncStatusSyncing {
			status.Status = clusterv1alpha2.SyncStatusSyncing
			status.LastTransitionTime = metav1.Now().Rfc3339Copy()
			synchro.status.Store(status)
		}

		synchro.handleResourceEvent(event)

		// if the queue size is empty that represent the resource had be synced by synchronizer.
		if isEmpty, err := synchro.queue.Done(event); err != nil && isEmpty {
			status.Status = clusterv1alpha2.SyncStatusSynced
			status.LastTransitionTime = metav1.Now().Rfc3339Copy()
			synchro.status.Store(status)
		}
	}
}

func (synchro *ResourceSynchro) handleResourceEvent(event *queue.Event) {
	if d, ok := event.Object.(cache.DeletedFinalStateUnknown); ok {
		namespace, name, err := cache.SplitMetaNamespaceKey(d.Key)
		if err != nil {
			klog.Error(err)
			return
		}
		obj := &metav1.PartialObjectMetadata{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
		}

		if err := synchro.deleteResource(obj); err != nil {
			klog.ErrorS(err, "Failed to handler resource",
				"cluster", synchro.cluster,
				"action", event.Action,
				"resource", synchro.storageResource,
				"namespace", namespace,
				"name", name,
			)
		}
		return
	}

	var err error
	obj := event.Object.(runtime.Object)

	// if synchro.convertor == nil, it means no conversion is needed.
	if synchro.convertor != nil {
		if obj, err = synchro.convertToStorageVersion(obj); err != nil {
			klog.Error(err)
			return
		}
	}
	utils.InjectClusterName(obj, synchro.cluster)

	switch event.Action {
	case queue.Added:
		err = synchro.createOrUpdateResource(obj)
	case queue.Updated:
		err = synchro.updateOrCreateResource(obj)
	case queue.Deleted:
		err = synchro.deleteResource(obj)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		o, _ := meta.Accessor(obj)
		klog.ErrorS(err, "Failed to handler resource",
			"cluster", synchro.cluster,
			"action", event.Action,
			"resource", synchro.storageResource,
			"namespace", o.GetNamespace(),
			"name", o.GetName(),
		)
	}
}

func (synchro *ResourceSynchro) convertToStorageVersion(obj runtime.Object) (runtime.Object, error) {
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

func (synchro *ResourceSynchro) createOrUpdateResource(obj runtime.Object) error {
	err := synchro.storage.Create(synchro.ctx, synchro.cluster, obj)
	if genericstorage.IsNodeExist(err) {
		return synchro.storage.Update(synchro.ctx, synchro.cluster, obj)
	}
	return err
}

func (synchro *ResourceSynchro) updateOrCreateResource(obj runtime.Object) error {
	err := synchro.storage.Update(synchro.ctx, synchro.cluster, obj)
	if genericstorage.IsNotFound(err) {
		return synchro.storage.Create(synchro.ctx, synchro.cluster, obj)
	}
	return err
}

func (synchro *ResourceSynchro) deleteResource(obj runtime.Object) error {
	return synchro.storage.Delete(synchro.ctx, synchro.cluster, obj)
}

func (synchro *ResourceSynchro) Status() clusterv1alpha2.ClusterResourceSyncCondition {
	return synchro.status.Load().(clusterv1alpha2.ClusterResourceSyncCondition)
}
