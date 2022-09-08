package memorystorage

import (
	"context"
	"sync"

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
	// todo
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
	// todo
	return nil
}
