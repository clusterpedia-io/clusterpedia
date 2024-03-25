package internalstorage

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type StorageFactory struct {
	db                *gorm.DB
	SkipAutoMigration bool
	DivisionPolicy    DivisionPolicy
}

func (s *StorageFactory) AutoMigrate() error {
	return nil
}

func (s *StorageFactory) GetSupportedRequestVerbs() []string {
	return []string{"get", "list"}
}

func (s *StorageFactory) NewResourceStorage(config *storage.ResourceStorageConfig) (storage.ResourceStorage, error) {
	gvr := schema.GroupVersionResource{
		Group:    config.StorageGroupResource.Group,
		Version:  config.StorageVersion.Version,
		Resource: config.StorageGroupResource.Resource,
	}
	table := s.tableName(gvr)

	var model interface{}
	switch s.DivisionPolicy {
	case DivisionPolicyGroupVersionResource:
		model = &GroupVersionResource{}
		if !s.SkipAutoMigration {
			if exist := s.db.Migrator().HasTable(table); !exist {
				if err := s.db.AutoMigrate(&GroupVersionResource{}); err != nil {
					return nil, err
				}

				if err := s.db.Migrator().RenameTable("group_version_resources", table); err != nil {
					if !s.db.Migrator().HasTable(table) {
						return nil, err
					}
				}
			}
		}
	default:
		model = &Resource{}
	}

	return &ResourceStorage{
		db:                   s.db.Table(table).Model(model),
		codec:                config.Codec,
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
	tables, err := s.db.Migrator().GetTables()
	if err != nil {
		return nil, err
	}

	var resources []Resource
	for _, table := range tables {
		result := s.db.WithContext(ctx).
			Table(table).
			Select("group", "version", "resource", "namespace", "name", "resource_version").
			Where(map[string]interface{}{"cluster": cluster}).
			Find(&resources)
		if result.Error != nil {
			return nil, InterpretDBError(cluster, result.Error)
		}
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
	tables, err := s.db.Migrator().GetTables()
	if err != nil {
		return err
	}

	for _, table := range tables {
		result := s.db.WithContext(ctx).Table(table).Where(map[string]interface{}{"cluster": cluster}).Delete(&Resource{})
		if result.Error != nil {
			return InterpretDBError(cluster, result.Error)
		}
	}

	return nil
}

func (s *StorageFactory) CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error {
	err := s.db.Transaction(func(db *gorm.DB) error {
		result := s.db.WithContext(ctx).
			Table(s.tableName(gvr)).
			Where(map[string]interface{}{
				"cluster":  cluster,
				"group":    gvr.Group,
				"version":  gvr.Version,
				"resource": gvr.Resource,
			}).
			Delete(&Resource{})

		if result.Error != nil {
			return result.Error
		}

		return nil
	})

	return InterpretDBError(fmt.Sprintf("%s/%s", cluster, gvr), err)
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

// GenerateTableFor return table name using gvr string
func GenerateTableFor(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return fmt.Sprintf("%s_%s", gvr.Version, gvr.Resource)
	}

	group := strings.ReplaceAll(gvr.Group, ".", "_")
	return fmt.Sprintf("%s_%s_%s", group, gvr.Version, gvr.Resource)
}

func (s *StorageFactory) tableName(gvr schema.GroupVersionResource) string {
	table := "resources"
	if s.DivisionPolicy == DivisionPolicyGroupVersionResource {
		table = GenerateTableFor(gvr)
	}

	return table
}
