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
		groupResource: config.StorageResource.GroupResource(),

		db:     s.db,
		config: *config,
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

func (s *StorageFactory) GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]storage.ClusterResourceVersions, error) {
	var resources []Resource
	result := s.db.WithContext(ctx).Select("group", "version", "resource", "namespace", "name", "resource_version", "event_resource_versions").
		Where(map[string]interface{}{"cluster": cluster}).
		Find(&resources)
	if result.Error != nil {
		return nil, InterpretDBError(cluster, result.Error)
	}

	resourceversions := make(map[schema.GroupVersionResource]storage.ClusterResourceVersions)
	for _, resource := range resources {
		gvr := resource.GroupVersionResource()
		versions, ok := resourceversions[gvr]
		if !ok {
			versions = storage.ClusterResourceVersions{
				Resources: make(map[string]interface{}),
				Events:    make(map[string]interface{}, 0),
			}
			resourceversions[gvr] = versions
		}

		key := resource.Name
		if resource.Namespace != "" {
			key = resource.Namespace + "/" + resource.Name
		}
		versions.Resources[key] = resource.ResourceVersion
		for k, v := range resource.EventResourceVersions {
			versions.Events[k] = v
		}
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

func (s *StorageFactory) Shutdown() error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
