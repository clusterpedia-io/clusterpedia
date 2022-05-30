package internalstorage

import (
	"context"
	"sort"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

var caseSensitiveJSONIterator = json.CaseSensitiveJSONIterator()

type CollectionResourceStorage struct {
	db         *gorm.DB
	typesQuery *gorm.DB

	collectionResource *internal.CollectionResource
}

func NewCollectionResourceStorage(db *gorm.DB, cr *internal.CollectionResource) storage.CollectionResourceStorage {
	typesQuery := db
	groups := make([]string, 0)
	for _, rt := range cr.ResourceTypes {
		if rt.Resource == "" && rt.Version == "" {
			groups = append(groups, rt.Group)
			continue
		}

		where := map[string]interface{}{"group": rt.Group}
		if rt.Resource != "" {
			where["resource"] = rt.Resource
		}
		if rt.Version != "" {
			where["version"] = rt.Version
		}
		typesQuery = typesQuery.Or(where)
	}
	if len(groups) != 0 {
		typesQuery = typesQuery.Or(map[string]interface{}{"group": groups})
	}

	return &CollectionResourceStorage{
		db:                 db,
		typesQuery:         typesQuery,
		collectionResource: cr.DeepCopy(),
	}
}

func (s *CollectionResourceStorage) query(ctx context.Context, metadata bool) (*gorm.DB, ObjectList) {
	query := s.db.WithContext(ctx).Model(&Resource{})
	if !metadata {
		return query.Where(s.typesQuery), &ResourceList{}
	}

	switch s.db.Dialector.Name() {
	case "mysql":
		query = query.Select("`group`, version, resource, kind, object->>'$.metadata' as metadata")
	case "postgres":
		query = query.Select(`"group", version, resource, kind, object->>'metadata' as metadata`)
	}
	return query.Where(s.typesQuery), &ResourceMetadataList{}
}

func (s *CollectionResourceStorage) Get(ctx context.Context, opts *internal.ListOptions) (*internal.CollectionResource, error) {
	query, list := s.query(ctx, opts.OnlyMetadata)
	_, query, err := applyListOptionsToCollectionResourceQuery(query, opts)
	if err != nil {
		return nil, err
	}

	if err := list.From(query); err != nil {
		return nil, InterpreError(s.collectionResource.Name, err)
	}

	gvrs := make(map[schema.GroupVersionResource]struct{})
	types := []internal.CollectionResourceType{}
	objs := make([]runtime.Object, 0)
	for _, resource := range list.Items() {
		obj, err := resource.ConvertToUnstructured()
		if err != nil {
			return nil, InterpreError(s.collectionResource.Name, err)
		}
		objs = append(objs, obj)

		resourceType := resource.GetResourceType()
		if _, ok := gvrs[resourceType.GroupVersionResource()]; !ok {
			types = append(types, internal.CollectionResourceType{
				Group:    resourceType.Group,
				Resource: resourceType.Resource,
				Version:  resourceType.Version,
				Kind:     resourceType.Kind,
			})
		}
	}
	sortCollectionResourceTypes(types)

	return &internal.CollectionResource{
		TypeMeta:      s.collectionResource.TypeMeta,
		ObjectMeta:    s.collectionResource.ObjectMeta,
		ResourceTypes: types,
		Items:         objs,
	}, nil
}

// TODO(iceber): support with remaining count and continue
func applyListOptionsToCollectionResourceQuery(query *gorm.DB, opts *internal.ListOptions) (int64, *gorm.DB, error) {
	return applyListOptionsToQuery(query, opts, nil)
}

func sortCollectionResourceTypes(types []internal.CollectionResourceType) {
	sort.Slice(types, func(i, j int) bool {
		left, right := types[i], types[j]
		return left.Group > right.Group && left.Resource > right.Resource
	})
}
