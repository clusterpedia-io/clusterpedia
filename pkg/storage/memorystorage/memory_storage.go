package memorystorage

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	cache "github.com/clusterpedia-io/clusterpedia/pkg/storage/memorystorage/watchcache"
)

type StorageFactory struct {
}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig) (storage.ResourceStorage, error) {
	storages.Lock()
	defer storages.Unlock()

	gvr := schema.GroupVersionResource{
		Group:    config.GroupResource.Group,
		Version:  config.StorageVersion.Version,
		Resource: config.GroupResource.Resource,
	}

	if resourceStorage, ok := storages.resourceStorages[gvr]; ok {
		watchCache := resourceStorage.watchCache
		if config.Namespaced {
			watchCache.KeyFunc = cache.GetKeyFunc(gvr, config.Namespaced)
			watchCache.IsNamespaced = true
		}
		if _, ok := watchCache.GetStores()[config.Cluster]; !ok {
			resourceStorage.watchCache.AddIndexer(config.Cluster, nil)
		}

		return resourceStorage, nil
	}

	watchCache := cache.NewWatchCache(100, gvr, config.Namespaced, config.Cluster)
	config.WatchCache = watchCache
	resourceStorage := &ResourceStorage{
		incoming:      make(chan ClusterWatchEvent, 100),
		Codec:         config.Codec,
		watchCache:    watchCache,
		storageConfig: config,
	}

	storages.resourceStorages[gvr] = resourceStorage

	return resourceStorage, nil
}

func (s *StorageFactory) NewCollectionResourceStorage(cr *internal.CollectionResource) (storage.CollectionResourceStorage, error) {
	return nil, nil
}

func (s *StorageFactory) GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error) {
	return nil, nil
}

func (s *StorageFactory) CleanCluster(ctx context.Context, cluster string) error {
	return nil
}

func (s *StorageFactory) CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error {
	return nil
}

func (s *StorageFactory) GetCollectionResources(ctx context.Context) ([]*internal.CollectionResource, error) {
	return nil, nil
}
