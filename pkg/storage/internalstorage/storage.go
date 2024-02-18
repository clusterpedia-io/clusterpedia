package internalstorage

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type StorageFactory struct {
	db *gorm.DB
}

func (s *StorageFactory) GetSupportedRequestVerbs() []string {
	return []string{"get", "list"}
}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig) (storage.ResourceStorage, error) {
	return &ResourceStorage{
		db:    s.db,
		codec: config.Codec,

		storageGroupResource: config.StorageGroupResource,
		storageVersion:       config.StorageVersion,
		memoryVersion:        config.MemoryVersion,
	}, nil
}

func (s *StorageFactory) NewCollectionResourceStorage(cr *internal.CollectionResource) (storage.CollectionResourceStorage, error) {
	for i := range collectionResources {
		if collectionResources[i].Name == cr.Name {
			return NewCollectionResourceStorage(s.db, cr), nil
		}
	}
	return nil, fmt.Errorf("not support collection resource: %s", cr.Name)
}

func (s *StorageFactory) GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error) {
	var resources []Resource
	result := s.db.WithContext(ctx).Select("group", "version", "resource", "namespace", "name", "resource_version").
		Where(map[string]interface{}{"cluster": cluster}).
		Find(&resources)
	if result.Error != nil {
		return nil, InterpretDBError(cluster, result.Error)
	}

	resourceversions := make(map[schema.GroupVersionResource]map[string]interface{})
	for _, resource := range resources {
		gvr := resource.GroupVersionResource()
		versions := resourceversions[gvr]
		if versions == nil {
			versions = make(map[string]interface{})
			resourceversions[gvr] = versions
		}

		key := resource.Name
		if resource.Namespace != "" {
			key = resource.Namespace + "/" + resource.Name
		}
		versions[key] = resource.ResourceVersion
	}
	return resourceversions, nil
}

func (s *StorageFactory) CleanCluster(ctx context.Context, cluster string) error {
	result := s.db.WithContext(ctx).Where(map[string]interface{}{"cluster": cluster}).Delete(&Resource{})
	return InterpretDBError(cluster, result.Error)
}

func (s *StorageFactory) CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error {
	result := s.db.WithContext(ctx).Where(map[string]interface{}{
		"cluster":  cluster,
		"group":    gvr.Group,
		"version":  gvr.Version,
		"resource": gvr.Resource,
	}).Delete(&Resource{})
	return InterpretDBError(fmt.Sprintf("%s/%s", cluster, gvr), result.Error)
}

func (s *StorageFactory) GetCollectionResources(ctx context.Context) ([]*internal.CollectionResource, error) {
	var crs []*internal.CollectionResource
	for _, cr := range collectionResources {
		crs = append(crs, cr.DeepCopy())
	}
	return crs, nil
}

func (s *StorageFactory) PrepareCluster(cluster string) error {
	return nil
}
