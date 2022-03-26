package internalstorage

import (
	"context"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

var caseSensitiveJSONIterator = json.CaseSensitiveJSONIterator()

type CollectionResourceStorage struct {
	db *gorm.DB

	collectionResource *internal.CollectionResource
}

func (s *CollectionResourceStorage) Get(ctx context.Context, opts *internal.ListOptions) (*internal.CollectionResource, error) {
	cr := s.collectionResource.DeepCopy()
	types := make(map[schema.GroupResource]*internal.CollectionResourceType, len(cr.ResourceTypes))

	var typesQuery = s.db
	for i, rt := range cr.ResourceTypes {
		typesQuery = typesQuery.Or(map[string]interface{}{
			"group":    rt.Group,
			"version":  rt.Version,
			"resource": rt.Resource,
		})
		types[rt.GroupResource()] = &cr.ResourceTypes[i]
	}

	query := s.db.WithContext(ctx).Model(&Resource{}).Where(typesQuery)
	_, query, err := applyListOptionsToCollectionResourceQuery(query, opts)
	if err != nil {
		return nil, err
	}

	var resources []Resource
	result := query.Find(&resources)
	if result.Error != nil {
		return nil, InterpreError(s.collectionResource.Name, result.Error)
	}

	objs := make([]runtime.Object, 0, len(resources))
	for _, resource := range resources {
		types[resource.GroupVersionResource().GroupResource()].Kind = resource.Kind

		obj := &unstructured.Unstructured{}
		if err := caseSensitiveJSONIterator.Unmarshal(resource.Object, obj); err != nil {
			return nil, InterpreError(s.collectionResource.Name, err)
		}

		objs = append(objs, obj)
	}

	cr.Items = objs
	return cr, nil
}

// TODO(iceber): support with remaining count and continue
func applyListOptionsToCollectionResourceQuery(query *gorm.DB, opts *internal.ListOptions) (int64, *gorm.DB, error) {
	return applyListOptionsToQuery(query, opts, nil)
}
