package internalstorage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"

	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type ResourceStorage struct {
	db    *gorm.DB
	codec runtime.Codec

	storageGroupResource schema.GroupResource
	storageVersion       schema.GroupVersion
	memoryVersion        schema.GroupVersion
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return &storage.ResourceStorageConfig{
		Codec:                s.codec,
		StorageGroupResource: s.storageGroupResource,
		StorageVersion:       s.storageVersion,
		MemoryVersion:        s.memoryVersion,
	}
}

func (s *ResourceStorage) Create(ctx context.Context, cluster string, obj runtime.Object) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" {
		return fmt.Errorf("%s: kind is required", gvk)
	}

	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return InterpreError("", err)
	}

	var ownerUID types.UID
	if owner := metav1.GetControllerOfNoCopy(metaobj); owner != nil {
		ownerUID = owner.UID
	}

	var buffer bytes.Buffer
	if err := s.codec.Encode(obj, &buffer); err != nil {
		return InterpreResourceError(cluster, metaobj.GetName(), err)
	}

	resource := Resource{
		Cluster:         cluster,
		OwnerUID:        ownerUID,
		UID:             metaobj.GetUID(),
		Name:            metaobj.GetName(),
		Namespace:       metaobj.GetNamespace(),
		Group:           s.storageGroupResource.Group,
		Resource:        s.storageGroupResource.Resource,
		Version:         s.storageVersion.Version,
		Kind:            gvk.Kind,
		ResourceVersion: metaobj.GetResourceVersion(),
		Object:          buffer.Bytes(),
		CreatedAt:       metaobj.GetCreationTimestamp().Time,
	}
	if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
		resource.DeletedAt = sql.NullTime{Time: deletedAt.Time, Valid: true}
	}

	result := s.db.WithContext(ctx).Create(&resource)
	return InterpreResourceError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) Update(ctx context.Context, cluster string, obj runtime.Object) error {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return InterpreError("", err)
	}

	var buffer bytes.Buffer
	if err := s.codec.Encode(obj, &buffer); err != nil {
		return InterpreResourceError(cluster, metaobj.GetName(), err)
	}

	var ownerUID types.UID
	if owner := metav1.GetControllerOfNoCopy(metaobj); owner != nil {
		ownerUID = owner.UID
	}

	// The uid may not be the same for resources with the same namespace/name
	// in the same cluster at different times.
	updatedResource := map[string]interface{}{
		"owner_uid":        ownerUID,
		"uid":              metaobj.GetUID(),
		"resource_version": metaobj.GetResourceVersion(),
		"object":           buffer.Bytes(),
	}
	if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
		updatedResource["deleted_at"] = sql.NullTime{Time: deletedAt.Time, Valid: true}
	}

	result := s.db.WithContext(ctx).Model(&Resource{}).Where(map[string]interface{}{
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"namespace": metaobj.GetNamespace(),
		"name":      metaobj.GetName(),
	}).Updates(updatedResource)
	return InterpreResourceError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) deleteObject(cluster, namespace, name string) *gorm.DB {
	return s.db.Model(&Resource{}).Where(map[string]interface{}{
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"namespace": namespace,
		"name":      name,
	}).Delete(&Resource{})
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object) error {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return InterpreError("", err)
	}

	if result := s.deleteObject(cluster, metaobj.GetNamespace(), metaobj.GetName()); result.Error != nil {
		return InterpreResourceError(cluster, metaobj.GetName(), result.Error)
	}
	return nil
}

func (s *ResourceStorage) genGetObjectQuery(ctx context.Context, cluster, namespace, name string) *gorm.DB {
	return s.db.WithContext(ctx).Model(&Resource{}).Select("object").Where(map[string]interface{}{
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"namespace": namespace,
		"name":      name,
	})
}

func (s *ResourceStorage) Get(ctx context.Context, cluster, namespace, name string, into runtime.Object) error {
	var objects [][]byte
	if result := s.genGetObjectQuery(ctx, cluster, namespace, name).First(&objects); result.Error != nil {
		return InterpreResourceError(cluster, namespace+"/"+name, result.Error)
	}

	obj, _, err := s.codec.Decode(objects[0], nil, into)
	if err != nil {
		return InterpreResourceError(cluster, namespace+"/"+name, err)
	}
	if obj != into {
		err := fmt.Errorf("Failed to decode resource, into is %T", into)
		return InterpreResourceError(cluster, namespace+"/"+name, err)
	}
	return nil
}

func (s *ResourceStorage) genListObjectsQuery(ctx context.Context, opts *internal.ListOptions) (int64, *int64, *gorm.DB, error) {
	query := s.db.WithContext(ctx).Model(&Resource{}).Select("object").Where(map[string]interface{}{
		"group":    s.storageGroupResource.Group,
		"version":  s.storageVersion.Version,
		"resource": s.storageGroupResource.Resource,
	})
	return applyListOptionsToResourceQuery(s.db, query, opts)
}

func (s *ResourceStorage) List(ctx context.Context, listObject runtime.Object, opts *internal.ListOptions) error {
	offset, amount, query, err := s.genListObjectsQuery(ctx, opts)
	if err != nil {
		return err
	}

	var objects [][]byte
	if result := query.Find(&objects); result.Error != nil {
		return InterpreError(s.storageGroupResource.String(), result.Error)
	}

	list, err := meta.ListAccessor(listObject)
	if err != nil {
		return InterpreError(s.storageGroupResource.String(), err)
	}

	if opts.WithContinue != nil && *opts.WithContinue {
		if int64(len(objects)) == opts.Limit {
			list.SetContinue(strconv.FormatInt(offset+opts.Limit, 10))
		}
	}

	if amount != nil {
		// When offset is too large, the data in the response is empty and the remaining count is negative.
		// This ensures that `amount = offset + len(objects) + remain`
		remain := *amount - offset - int64(len(objects))
		list.SetRemainingItemCount(&remain)
	}

	listPtr, err := meta.GetItemsPtr(listObject)
	if err != nil {
		return InterpreError(s.storageGroupResource.String(), err)
	}

	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return InterpreError(s.storageGroupResource.String(), fmt.Errorf("need ptr to slice: %v", err))
	}

	newItemFunc := getNewItemFunc(listObject, v)
	for _, object := range objects {
		if err := appendListItem(v, object, s.codec, newItemFunc); err != nil {
			return InterpreError(s.storageGroupResource.String(), fmt.Errorf("need ptr to slice: %v", err))
		}
	}
	return nil
}

func applyListOptionsToResourceQuery(db *gorm.DB, query *gorm.DB, opts *internal.ListOptions) (int64, *int64, *gorm.DB, error) {
	var amount *int64
	applyFn := func(query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error) {
		query, err := applyOwnerToResourceQuery(db, query, opts)
		if err != nil {
			return nil, err
		}

		if opts.WithRemainingCount != nil && *opts.WithRemainingCount {
			amount = new(int64)
			query = query.Count(amount)
		}
		return query, nil
	}

	offset, query, err := applyListOptionsToQuery(query, opts, applyFn)
	if err != nil {
		return 0, nil, nil, err
	}
	return offset, amount, query, nil
}

func applyOwnerToResourceQuery(db *gorm.DB, query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error) {
	var ownerQuery interface{}
	switch {
	case len(opts.ClusterNames) != 1:
		return query, nil

	case opts.OwnerUID != "":
		ownerQuery = buildOwnerQueryByUID(db, opts.ClusterNames[0], opts.OwnerUID, opts.OwnerSeniority)

	case opts.OwnerName != "":
		var ownerNamespaces []string
		if len(opts.Namespaces) != 0 {
			// match namespaced and clustered owner resources
			ownerNamespaces = append(opts.Namespaces, "")
		}
		ownerQuery = buildOwnerQueryByName(db, opts.ClusterNames[0], ownerNamespaces, opts.OwnerGroupResource, opts.OwnerName, opts.OwnerSeniority)

	default:
		return query, nil
	}

	if _, ok := ownerQuery.(string); ok {
		query = query.Where("owner_uid = ?", ownerQuery)
	} else {
		query = query.Where("owner_uid IN (?)", ownerQuery)
	}
	return query, nil
}
