package memorystorage

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type StorageFactory struct{}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig) (storage.ResourceStorage, error) {
	return nil, nil
}

func (s *StorageFactory) GetSupportedRequestVerbs() []string {
	return []string{"get", "list", "watch"}
}

func (s *StorageFactory) PrepareCluster(cluster string) error {
	return nil
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

func (s *StorageFactory) Shutdown() error {
	return nil
}
