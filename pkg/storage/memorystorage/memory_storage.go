package memorystorage

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	cache "github.com/clusterpedia-io/clusterpedia/pkg/storage/memorystorage/watchcache"
)

type StorageFactory struct {
	clusters map[string]bool
}

func (s *StorageFactory) GetSupportedRequestVerbs() []string {
	return []string{"get", "list", "watch"}
}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig, _ bool) (storage.ResourceStorage, error) {
	storages.Lock()
	defer storages.Unlock()

	gvr := schema.GroupVersionResource{
		Group:    config.GroupResource.Group,
		Version:  config.StorageVersion.Version,
		Resource: config.GroupResource.Resource,
	}

	resourceStorage, ok := storages.resourceStorages[gvr]
	if ok {
		watchCache := resourceStorage.watchCache
		if config.Namespaced && !watchCache.IsNamespaced {
			watchCache.KeyFunc = cache.GetKeyFunc(gvr, config.Namespaced)
			watchCache.IsNamespaced = true
		}
	} else {
		watchCache := cache.NewWatchCache(100, gvr, config.Namespaced)
		resourceStorage = &ResourceStorage{
			incoming:      make(chan ClusterWatchEvent, 100),
			Codec:         config.Codec,
			watchCache:    watchCache,
			storageConfig: config,
		}

		storages.resourceStorages[gvr] = resourceStorage
	}

	for cluster := range s.clusters {
		resourceStorage.watchCache.AddIndexer(cluster, nil)

		if resourceStorage.CrvSynchro == nil {
			resourceStorage.CrvSynchro = cache.NewClusterResourceVersionSynchro(cluster)
		} else {
			resourceStorage.CrvSynchro.SetClusterResourceVersion(cluster, "0")
		}
	}

	return resourceStorage, nil
}

func (s *StorageFactory) PrepareCluster(cluster string) error {
	storages.Lock()
	defer storages.Unlock()

	if _, ok := s.clusters[cluster]; ok {
		return nil
	}

	s.clusters[cluster] = true
	return nil
}

func (s *StorageFactory) NewCollectionResourceStorage(cr *internal.CollectionResource) (storage.CollectionResourceStorage, error) {
	return nil, nil
}

func (s *StorageFactory) GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error) {
	return nil, nil
}

func (s *StorageFactory) CleanCluster(ctx context.Context, cluster string) error {
	storages.Lock()
	defer storages.Unlock()
	for _, rs := range storages.resourceStorages {
		rs.CrvSynchro.RemoveCluster(cluster)
		rs.watchCache.CleanCluster(cluster)
		delete(s.clusters, cluster)
	}

	return nil
}

func (s *StorageFactory) CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error {
	storages.Lock()
	defer storages.Unlock()
	if rs, ok := storages.resourceStorages[gvr]; ok {
		rs.CrvSynchro.RemoveCluster(cluster)
		rs.watchCache.CleanCluster(cluster)
		delete(s.clusters, cluster)
	}

	return nil
}

func (s *StorageFactory) GetCollectionResources(ctx context.Context) ([]*internal.CollectionResource, error) {
	return nil, nil
}
