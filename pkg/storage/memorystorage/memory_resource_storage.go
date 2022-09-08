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
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	internal "github.com/clusterpedia-io/api/clusterpedia"
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

//nolint
type ResourceStorage struct {
	sync.RWMutex

	Codec         runtime.Codec
	watchCache    *cache.WatchCache
	stopCh        chan struct{}
	incoming      chan ClusterWatchEvent
	storageConfig *storage.ResourceStorageConfig
}

//nolint
func (s *ResourceStorage) convertEvent(event *ClusterWatchEvent) error {
	s.Lock()
	s.Unlock()
	klog.V(10).Infof("event: %s", event)
	s.incoming <- *event
	return nil
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
	objects := s.watchCache.WaitUntilFreshAndList(opts)

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

	v.Set(slice)
	return nil
}
