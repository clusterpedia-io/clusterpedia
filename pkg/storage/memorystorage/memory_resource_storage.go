package memorystorage

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	cache "github.com/clusterpedia-io/clusterpedia/pkg/storage/memorystorage/watchcache"
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
	stopCh        chan struct{}
	crvSynchro    *cache.ClusterResourceVersionSynchro
	incoming      chan ClusterWatchEvent
	storageConfig *storage.ResourceStorageConfig
}

func (s *ResourceStorage) convertEvent(event *ClusterWatchEvent) error {
	klog.V(10).Infof("event: %s", event)
	s.crvSynchro.UpdateClusterResourceVersion(&event.Event, event.ClusterName)

	err := s.encodeEvent(&event.Event)
	if err != nil {
		return fmt.Errorf("encode event failed: %v", err)
	}
	s.incoming <- *event
	return nil
}

func (s *ResourceStorage) dispatchEvents() {
	for {
		select {
		case cwe, ok := <-s.incoming:
			if !ok {
				continue
			}

			switch cwe.Event.Type {
			case watch.Added:
				err := s.watchCache.Add(cwe.Event.Object, cwe.ClusterName)
				if err != nil {
					utilruntime.HandleError(fmt.Errorf("unable to add watch event object (%#v) to store: %v", cwe.Event.Object, err))
				}
			case watch.Modified:
				err := s.watchCache.Update(cwe.Event.Object, cwe.ClusterName)
				if err != nil {
					utilruntime.HandleError(fmt.Errorf("unable to update watch event object (%#v) to store: %v", cwe.Event.Object, err))
				}
			case watch.Deleted:
				// TODO: Will any consumers need access to the "last known
				// state", which is passed in event.Object? If so, may need
				// to change this.
				err := s.watchCache.Delete(cwe.Event.Object, cwe.ClusterName)
				if err != nil {
					utilruntime.HandleError(fmt.Errorf("unable to delete watch event object (%#v) from store: %v", cwe.Event.Object, err))
				}
			case watch.Error:
				s.watchCache.ClearWatchCache(cwe.ClusterName)
			default:
				utilruntime.HandleError(fmt.Errorf("unable to understand watch event %#v", cwe.Event))
				continue
			}
			s.dispatchEvent(&cwe.Event)
		case <-s.stopCh:
			return
		}
	}
}

func (s *ResourceStorage) dispatchEvent(event *watch.Event) {
	s.watchCache.WatchersLock.RLock()
	defer s.watchCache.WatchersLock.RUnlock()
	for _, watcher := range s.watchCache.WatchersBuffer {
		watcher.NonblockingAdd(event)
	}
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return s.storageConfig
}

func (s *ResourceStorage) Create(ctx context.Context, cluster string, obj runtime.Object) error {
	event := s.newClusterWatchEvent(watch.Added, obj, cluster)
	err := s.convertEvent(event)
	if err != nil {
		return err
	}
	return nil
}

func (s *ResourceStorage) Update(ctx context.Context, cluster string, obj runtime.Object) error {
	event := s.newClusterWatchEvent(watch.Modified, obj, cluster)
	err := s.convertEvent(event)
	if err != nil {
		return err
	}
	return nil
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object) error {
	event := s.newClusterWatchEvent(watch.Deleted, obj, cluster)
	err := s.convertEvent(event)
	if err != nil {
		return err
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
		return fmt.Errorf("Failed to decode resource, into is %T", into)
	}
	return nil
}

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

func (s *ResourceStorage) encodeEvent(event *watch.Event) error {
	var buffer bytes.Buffer

	gk := event.Object.GetObjectKind().GroupVersionKind().GroupKind()
	config := s.storageConfig
	if ok := resourcescheme.LegacyResourceScheme.IsGroupRegistered(gk.Group); ok {
		dest, err := resourcescheme.LegacyResourceScheme.New(config.MemoryVersion.WithKind(gk.Kind))
		if err != nil {
			return err
		}

		object := event.Object
		err = config.Codec.Encode(object, &buffer)
		if err != nil {
			return err
		}

		obj, _, err := config.Codec.Decode(buffer.Bytes(), nil, dest)
		if err != nil {
			return err
		}

		if obj != dest {
			return err
		}
		event.Object = dest
	}

	return nil
}
