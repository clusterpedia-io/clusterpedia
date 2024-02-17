package memorystorage

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	kubecache "k8s.io/client-go/tools/cache"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	cache "github.com/clusterpedia-io/clusterpedia/pkg/storage/memorystorage/watchcache"
	utilwatch "github.com/clusterpedia-io/clusterpedia/pkg/utils/watch"
)

var (
	storages *resourceStorages
)

type resourceStorages struct {
	sync.RWMutex
	resourceStorages map[schema.GroupVersionResource]*ResourceStorage
}

func init() {
	storages = &resourceStorages{
		resourceStorages: make(map[schema.GroupVersionResource]*ResourceStorage),
	}
}

type ClusterWatchEvent struct {
	Event       watch.Event
	ClusterName string
}

type ResourceStorage struct {
	sync.RWMutex

	Codec         runtime.Codec
	watchCache    *cache.WatchCache
	CrvSynchro    *cache.ClusterResourceVersionSynchro
	incoming      chan ClusterWatchEvent
	storageConfig *storage.ResourceStorageConfig
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return s.storageConfig
}

func (s *ResourceStorage) Create(ctx context.Context, cluster string, obj runtime.Object) error {
	resourceVersion, err := s.CrvSynchro.UpdateClusterResourceVersion(obj, cluster)
	if err != nil {
		return err
	}

	err = s.watchCache.Add(obj, cluster, resourceVersion, s.storageConfig.Codec, s.storageConfig.MemoryVersion)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to add watch event object (%#v) to store: %v", obj, err))
	}

	return nil
}

func (s *ResourceStorage) Update(ctx context.Context, cluster string, obj runtime.Object) error {
	resourceVersion, err := s.CrvSynchro.UpdateClusterResourceVersion(obj, cluster)
	if err != nil {
		return err
	}

	err = s.watchCache.Update(obj, cluster, resourceVersion, s.storageConfig.Codec, s.storageConfig.MemoryVersion)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to add watch event object (%#v) to store: %v", obj, err))
	}

	return nil
}

func (s *ResourceStorage) ConvertDeletedObject(obj interface{}) (runobj runtime.Object, _ error) {
	if d, ok := obj.(kubecache.DeletedFinalStateUnknown); ok {
		if obj, ok := d.Obj.(runtime.Object); ok {
			return obj, nil
		}
		namespace, name, err := kubecache.SplitMetaNamespaceKey(d.Key)
		if err != nil {
			return nil, err
		}
		return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}, nil
	}

	if obj, ok := obj.(runtime.Object); ok {
		return obj, nil
	}
	return nil, fmt.Errorf("invalid Type(%T): couldn't convert deleted object", obj)
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object) error {
	resourceVersion, err := s.CrvSynchro.UpdateClusterResourceVersion(obj, cluster)
	if err != nil {
		return err
	}

	err = s.watchCache.Delete(obj, cluster, resourceVersion, s.storageConfig.Codec, s.storageConfig.MemoryVersion)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to add watch event object (%#v) to store: %v", obj, err))
	}

	return nil
}

func (s *ResourceStorage) Get(ctx context.Context, cluster, namespace, name string, into runtime.Object) error {
	var buffer bytes.Buffer
	se, err := s.watchCache.WaitUntilFreshAndGet(cluster, namespace, name)
	if err != nil {
		return err
	}

	object := se.Object
	err = s.Codec.Encode(object, &buffer)
	if err != nil {
		return err
	}

	obj, _, err := s.Codec.Decode(buffer.Bytes(), nil, into)
	if err != nil {
		return err
	}

	if obj != into {
		return fmt.Errorf("failed to decode resource, into is %T", into)
	}
	return nil
}

// nolint
func (s *ResourceStorage) newClusterWatchEvent(eventType watch.EventType, obj runtime.Object, cluster string) *ClusterWatchEvent {
	return &ClusterWatchEvent{
		ClusterName: cluster,
		Event: watch.Event{
			Type:   eventType,
			Object: obj,
		},
	}
}

func (s *ResourceStorage) List(ctx context.Context, listObject runtime.Object, opts *internal.ListOptions) error {
	var buffer bytes.Buffer
	objects, readResourceVersion, err := s.watchCache.WaitUntilFreshAndList(opts)
	if err != nil {
		return err
	}

	list, err := meta.ListAccessor(listObject)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return nil
	}

	listPtr, err := meta.GetItemsPtr(listObject)
	if err != nil {
		return err
	}

	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	expected := reflect.New(v.Type().Elem()).Interface().(runtime.Object)
	seen := map[string]struct{}{}
	accessor := meta.NewAccessor()
	deduplicated := make([]runtime.Object, 0, len(objects))
	for _, object := range objects {
		buffer.Reset()
		obj := object.Object
		err = s.Codec.Encode(obj, &buffer)
		if err != nil {
			return err
		}

		obj, _, err := s.Codec.Decode(buffer.Bytes(), nil, expected.DeepCopyObject())
		if err != nil {
			return err
		}

		name, err := accessor.Name(obj)
		if err != nil {
			return err
		}

		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			deduplicated = append(deduplicated, obj)
		}
	}

	slice := reflect.MakeSlice(v.Type(), len(deduplicated), len(deduplicated))
	for i, obj := range deduplicated {
		slice.Index(i).Set(reflect.ValueOf(obj).Elem())
	}

	list.SetResourceVersion(readResourceVersion.GetClusterResourceVersion())
	v.Set(slice)
	return nil
}

func (s *ResourceStorage) Watch(ctx context.Context, options *internal.ListOptions) (watch.Interface, error) {
	resourceversion := options.ResourceVersion
	watchRV, err := cache.NewClusterResourceVersionFromString(resourceversion)
	if err != nil {
		// To match the uncached watch implementation, once we have passed authn/authz/admission,
		// and successfully parsed a resource version, other errors must fail with a watch event of type ERROR,
		// rather than a directly returned error.
		return newErrWatcher(err), nil
	}

	watcher := cache.NewCacheWatcher(100)
	watchCache := s.watchCache
	watchCache.Lock()
	defer watchCache.Unlock()

	initEvents, err := watchCache.GetAllEventsSinceThreadUnsafe(watchRV)
	if err != nil {
		// To match the uncached watch implementation, once we have passed authn/authz/admission,
		// and successfully parsed a resource version, other errors must fail with a watch event of type ERROR,
		// rather than a directly returned error.
		return newErrWatcher(err), nil
	}

	func() {
		watchCache.WatchersLock.Lock()
		defer watchCache.WatchersLock.Unlock()

		watchCache.WatchersBuffer = append(watchCache.WatchersBuffer, watcher)
	}()

	go watcher.Process(ctx, initEvents)
	return watcher, nil
}

type errWatcher struct {
	result chan watch.Event
}

func newErrWatcher(err error) *errWatcher {
	errEvent := utilwatch.NewErrorEvent(err)

	// Create a watcher with room for a single event, populate it, and close the channel
	watcher := &errWatcher{result: make(chan watch.Event, 1)}
	watcher.result <- errEvent
	close(watcher.result)

	return watcher
}

func (c *errWatcher) ResultChan() <-chan watch.Event {
	return c.result
}

func (c *errWatcher) Stop() {
	// no-op
}
