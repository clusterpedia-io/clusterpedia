package internalstorage

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

const (
	URLQueryGroups    = "groups"
	URLQueryResources = "resources"
)

type CollectionResourceStorage struct {
	db         *gorm.DB
	typesQuery *gorm.DB

	collectionResource *internal.CollectionResource
}

func NewCollectionResourceStorage(db *gorm.DB, cr *internal.CollectionResource) storage.CollectionResourceStorage {
	storage := &CollectionResourceStorage{db: db, collectionResource: cr.DeepCopy()}
	if len(cr.ResourceTypes) == 0 {
		return storage
	}

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
	storage.typesQuery = typesQuery
	return storage
}

func (s *CollectionResourceStorage) query(ctx context.Context, opts *internal.ListOptions) (*gorm.DB, ObjectList, error) {
	var result ObjectList = &ResourceList{}
	if opts.OnlyMetadata {
		result = &ResourceMetadataList{}
	}

	query := s.db.WithContext(ctx).Model(&Resource{})
	if s.typesQuery != nil {
		return query.Where(s.typesQuery), result, nil
	}

	// The `URLQueryGroups` and `URLQueryResources` only works on *Any Collection Resource*,
	// does it nned to work on other collection resources?
	gvrs, all, err := resolveGVRsFromURLQuery(opts.URLQuery)
	if err != nil {
		return nil, nil, apierrors.NewBadRequest(err.Error())
	}
	if len(gvrs) == 0 && !all {
		return nil, nil, apierrors.NewBadRequest("url query - `groups` or `resources` is required")
	}

	if all {
		return query, result, nil
	}

	typesQuery := s.db
	for _, gvr := range gvrs {
		where := map[string]interface{}{"group": gvr.Group}
		if gvr.Version != "" {
			where["version"] = gvr.Version
		}
		if gvr.Resource != "" {
			where["resource"] = gvr.Resource
		}
		typesQuery = typesQuery.Or(where)
	}
	return query.Where(typesQuery), result, nil
}

func (s *CollectionResourceStorage) Get(ctx context.Context, opts *internal.ListOptions) (*internal.CollectionResource, error) {
	query, list, err := s.query(ctx, opts)
	if err != nil {
		return nil, err
	}
	offset, amount, query, err := applyListOptionsToCollectionResourceQuery(query, opts)
	if err != nil {
		return nil, err
	}

	if err := list.From(query); err != nil {
		return nil, InterpretDBError(s.collectionResource.Name, err)
	}
	items := list.Items()
	collection := &internal.CollectionResource{
		TypeMeta:   s.collectionResource.TypeMeta,
		ObjectMeta: s.collectionResource.ObjectMeta,
		Items:      make([]runtime.Object, 0, len(items)),
	}

	gvrs := make(map[schema.GroupVersionResource]struct{})
	for _, resource := range items {
		obj, err := resource.ConvertToUnstructured()
		if err != nil {
			return nil, err
		}
		collection.Items = append(collection.Items, obj)

		if resourceType := resource.GetResourceType(); !resourceType.Empty() {
			gvr := resourceType.GroupVersionResource()
			if _, ok := gvrs[gvr]; !ok {
				gvrs[gvr] = struct{}{}
				collection.ResourceTypes = append(collection.ResourceTypes, internal.CollectionResourceType{
					Group:    resourceType.Group,
					Resource: resourceType.Resource,
					Version:  resourceType.Version,
					Kind:     resourceType.Kind,
				})
			}
		}
	}

	if opts.WithContinue != nil && *opts.WithContinue {
		if int64(len(items)) == opts.Limit {
			collection.Continue = strconv.FormatInt(offset+opts.Limit, 10)
		}
	}

	if amount != nil {
		// When offset is too large, the data in the response is empty and the remaining count is negative.
		// This ensures that `amount = offset + len(objects) + remain`
		remain := *amount - offset - int64(len(items))
		collection.RemainingItemCount = &remain
	}

	return collection, nil
}

func resolveGVRsFromURLQuery(query url.Values) (gvrs []schema.GroupVersionResource, all bool, err error) {
	if query.Has(URLQueryGroups) {
		for _, group := range strings.Split(query.Get(URLQueryGroups), ",") {
			if group == "*" {
				return nil, true, nil
			}

			gv, err := parseGroupVersion(group)
			if err != nil {
				return nil, false, fmt.Errorf("%s query: %w", URLQueryGroups, err)
			}

			gvrs = append(gvrs, gv.WithResource(""))
		}
	}
	if query.Has(URLQueryResources) {
		for _, resource := range strings.Split(query.Get(URLQueryResources), ",") {
			gvr, err := parseGroupVersionResource(resource)
			if err != nil {
				return nil, false, fmt.Errorf("%s query: %w", URLQueryResources, err)
			}

			gvrs = append(gvrs, gvr)
		}
	}
	return
}

func parseGroupVersion(gv string) (schema.GroupVersion, error) {
	gv = strings.ReplaceAll(gv, " ", "")
	if (len(gv) == 0) || (gv == "/") {
		// match legacy group
		return schema.GroupVersion{}, nil
	}

	strs := strings.Split(gv, "/")
	switch len(strs) {
	case 1:
		/*
			match:
				* "group"
		*/
		return schema.GroupVersion{Group: strs[0]}, nil
	case 2:
		/*
			match:
				* "/"
				* "group/version"
				* "/version"
				* "group/"
		*/
		return schema.GroupVersion{Group: strs[0], Version: strs[1]}, nil
	}
	return schema.GroupVersion{}, fmt.Errorf("unexpected GroupVersion string: %v, expect <group> or <group>/<version>", gv)
}

func parseGroupVersionResource(gvr string) (schema.GroupVersionResource, error) {
	gvr = strings.ReplaceAll(gvr, " ", "")
	if gvr == "" {
		return schema.GroupVersionResource{}, fmt.Errorf("unexpected GroupVersionResource string: %v, expect <group>/<resource> or <group>/<version>/<resource>", gvr)
	}

	strs := strings.Split(gvr, "/")
	switch len(strs) {
	case 2:
		/*
			match:
				* "group/resource"
				* "/resource" in legacy group /api
			not match:
				* "/"
				* "group/"
		*/
		if strs[1] != "" {
			return schema.GroupVersionResource{Group: strs[0], Resource: strs[1]}, nil
		}
	case 3:
		/*
			match:
				* "group/version/resource"
				* "/version/resource"
				* "//resource"
				* "group//resource"
			not match:
				* "group/version/"
				* "group//"
		*/
		if strs[2] != "" {
			return schema.GroupVersionResource{Group: strs[0], Version: strs[1], Resource: strs[2]}, nil
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("unexpected GroupVersionResource string: %v, expect <group>/<resource> or <group>/<version>/<resource>", gvr)
}

func applyListOptionsToCollectionResourceQuery(query *gorm.DB, opts *internal.ListOptions) (int64, *int64, *gorm.DB, error) {
	return applyListOptionsToQuery(query, opts, nil)
}
