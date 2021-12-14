package clustersynchro

import (
	"context"
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

	clustersv1alpha1 "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusters/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/queue"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/features"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

type ResourceSynchro struct {
	cluster         string
	storageResource schema.GroupResource

	queue         queue.EventQueue
	listerWatcher cache.ListerWatcher
	cache         *informer.ResourceVersionStorage

	memoryVersion schema.GroupVersion
	convertor     runtime.ObjectConvertor
	storage       storage.ResourceStorage
	status        atomic.Value // clustersv1alpha1.ClusterResourceSyncCondition

	runlock sync.Mutex
	stoped  chan struct{}

	closeOnce sync.Once
	ctx       context.Context
	cancel    context.CancelFunc
	closer    chan struct{}
	closed    chan struct{}
}

func newResourceSynchro(cluster string, lw cache.ListerWatcher, rvcache *informer.ResourceVersionStorage,
	convertor runtime.ObjectConvertor, storage storage.ResourceStorage,
) *ResourceSynchro {
	ctx, cancel := context.WithCancel(context.Background())
	synchro := &ResourceSynchro{
		cluster:         cluster,
		storageResource: storage.GetStorageConfig().StorageGroupResource,

		listerWatcher: lw,
		cache:         rvcache,
		queue:         queue.NewPressureQueue(cache.DeletionHandlingMetaNamespaceKeyFunc),

		storage:       storage,
		convertor:     convertor,
		memoryVersion: storage.GetStorageConfig().MemoryVersion,

		ctx:    ctx,
		cancel: cancel,
		stoped: make(chan struct{}),
		closer: make(chan struct{}),
		closed: make(chan struct{}),
	}
	close(synchro.stoped)

	status := clustersv1alpha1.ClusterResourceSyncCondition{
		Status:             clustersv1alpha1.SyncStatusPending,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)

	go synchro.storager(1)
	return synchro
}

func (synchro *ResourceSynchro) Run(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			return
		case <-synchro.closer:
			return
		case <-synchro.stoped:
		}

		var dorun bool
		func() {
			synchro.runlock.Lock()
			defer synchro.runlock.Unlock()

			select {
			case <-stopCh:
				return
			case <-synchro.closer:
				return
			default:
			}

			select {
			case <-synchro.stoped:
				dorun = true
				synchro.stoped = make(chan struct{})
			default:
			}
		}()

		if dorun {
			break
		}
	}

	defer close(synchro.stoped)

	informerStopCh := make(chan struct{})
	go func() {
		select {
		case <-stopCh:
		case <-synchro.closer:
		}
		close(informerStopCh)
	}()

	status := clustersv1alpha1.ClusterResourceSyncCondition{
		Status:             clustersv1alpha1.SyncStatusSyncing,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)

	informer.NewResourceVersionInformer(
		synchro.cluster,
		synchro.listerWatcher,
		synchro.cache,
		&unstructured.Unstructured{},
		synchro,
	).Run(informerStopCh)

	status = clustersv1alpha1.ClusterResourceSyncCondition{
		Status:             clustersv1alpha1.SyncStatusStop,
		LastTransitionTime: metav1.Now().Rfc3339Copy(),
	}
	synchro.status.Store(status)
}

func (synchro *ResourceSynchro) Close() {
	synchro.closeOnce.Do(func() {
		close(synchro.closer)
		synchro.queue.Close()
		synchro.cancel()
	})

	<-synchro.closed
	klog.InfoS("resource synchro  is closed", "cluster", synchro.cluster, "resource", synchro.storageResource)
	//klog.V(2).InfoS("resource synchro  is closed", "cluster", synchro.cluster, "resource", synchro.storageResource)
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

	synchro.queue.Add(obj)
}

func (synchro *ResourceSynchro) OnUpdate(_, obj interface{}) {
	// `obj` will not be processed in parallel elsewhere,
	// no deep copy is needed for now.
	//
	// robj := obj.(runtime.Object).DeepCopyObject()

	// https://github.com/clusterpedia-io/clusterpedia/issues/4
	synchro.pruneObject(obj.(*unstructured.Unstructured))
	synchro.queue.Update(obj)
}

func (synchro *ResourceSynchro) OnDelete(obj interface{}) {
	synchro.queue.Delete(obj)
}

func (synchro *ResourceSynchro) OnSync(obj interface{}) {
}

func (synchro *ResourceSynchro) storager(worker int) {
	var waitGroup sync.WaitGroup
	for i := 0; i < worker; i++ {
		waitGroup.Add(1)

		go wait.Until(func() {
			defer waitGroup.Done()

			synchro.processResources()
		}, time.Second, synchro.closer)
	}

	waitGroup.Wait()
	close(synchro.closed)
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

		synchro.handleResourceEvent(event)
	}
}

func (synchro *ResourceSynchro) handleResourceEvent(event *queue.Event) {
	defer synchro.queue.Done(event)

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

		synchro.deleteResource(obj)
		return
	}

	var err error
	obj := event.Object.(runtime.Object)
	if synchro.convertor != nil {
		obj, err = synchro.convertor.ConvertToVersion(obj, synchro.memoryVersion)
		if err != nil {
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

	if err != nil {
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

func (synchro *ResourceSynchro) Status() clustersv1alpha1.ClusterResourceSyncCondition {
	return synchro.status.Load().(clustersv1alpha1.ClusterResourceSyncCondition)
}
