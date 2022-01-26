package internalstorage

import (
	"context"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"

	pediainternal "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
)

var caseSensitiveJSONIterator = json.CaseSensitiveJSONIterator()

type CollectionResourceStorage struct {
	db *gorm.DB

	collectionResource *pediainternal.CollectionResource
}

func (s *CollectionResourceStorage) Get(ctx context.Context, opts *pediainternal.ListOptions) (*pediainternal.CollectionResource, error) {
	cr := s.collectionResource.DeepCopy()

	types := make(map[schema.GroupResource]*pediainternal.CollectionResourceType, len(cr.ResourceTypes))
	query := s.db.WithContext(ctx).Model(&Resource{}).Where(map[string]interface{}{
		"group":    cr.ResourceTypes[0].Group,
		"version":  cr.ResourceTypes[0].Version,
		"resource": cr.ResourceTypes[0].Resource,
	})
	types[cr.ResourceTypes[0].GroupResource()] = &cr.ResourceTypes[0]
	for i, rt := range cr.ResourceTypes[1:] {
		query.Or(map[string]interface{}{
			"group":    rt.Group,
			"version":  rt.Version,
			"resource": rt.Resource,
		})
		types[rt.GroupResource()] = &cr.ResourceTypes[i]
	}

	// TODO(iceber): support with remaining count and continue
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

func applyListOptionsToCollectionResourceQuery(query *gorm.DB, opts *pediainternal.ListOptions) (int64, *gorm.DB, error) {
	return applyListOptionsToQuery(query, opts, nil)
}
