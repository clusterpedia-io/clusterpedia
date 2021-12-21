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
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	pediainternal "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type ResourceStorage struct {
	db    *gorm.DB
	codec runtime.Codec

	storageGroupResource schema.GroupResource
	storageVersion       schema.GroupVersion
	memoryVersion        schema.GroupVersion
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

	var buffer bytes.Buffer
	if err := s.codec.Encode(obj, &buffer); err != nil {
		return InterpreResourceError(cluster, metaobj.GetName(), err)
	}

	resource := Resource{
		Cluster:         cluster,
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

	result := s.db.Create(&resource)
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

	updatedResource := Resource{
		ResourceVersion: metaobj.GetResourceVersion(),
		Object:          buffer.Bytes(),
	}
	if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
		updatedResource.DeletedAt = sql.NullTime{Time: deletedAt.Time, Valid: true}
	}

	resource := Resource{
		Cluster:   cluster,
		Name:      metaobj.GetName(),
		UID:       metaobj.GetUID(),
		Namespace: metaobj.GetNamespace(),
		Group:     s.storageGroupResource.Group,
		Resource:  s.storageGroupResource.Resource,
		Version:   s.storageVersion.Version,
	}
	result := s.db.Where(&resource).Updates(&updatedResource)
	return InterpreResourceError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object) error {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return InterpreError("", err)
	}

	resource := Resource{
		Cluster:   cluster,
		Name:      metaobj.GetName(),
		Namespace: metaobj.GetNamespace(),
		Group:     s.storageGroupResource.Group,
		Resource:  s.storageGroupResource.Resource,
		Version:   s.storageVersion.Version,
	}

	result := s.db.Where(&resource).Delete(&Resource{})
	return InterpreResourceError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) Get(ctx context.Context, cluster, namespace, name string, into runtime.Object) error {
	resource := Resource{
		Cluster:   cluster,
		Namespace: namespace,
		Name:      name,
		Group:     s.storageGroupResource.Group,
		Resource:  s.storageGroupResource.Resource,
		Version:   s.storageVersion.Version,
	}

	result := s.db.WithContext(ctx).Select("object").Where(&resource).First(&resource)
	if result.Error != nil {
		return InterpreResourceError(cluster, namespace+"/"+name, result.Error)
	}

	obj, _, err := s.codec.Decode(resource.Object, nil, into)
	if err != nil {
		return InterpreResourceError(cluster, namespace+"/"+name, err)
	}
	if obj != into {
		err := fmt.Errorf("Failed to decode resource, into is %T", into)
		return InterpreResourceError(cluster, namespace+"/"+name, err)
	}
	return nil
}

func (s *ResourceStorage) List(ctx context.Context, listObject runtime.Object, opts *pediainternal.ListOptions) error {
	query := s.db.WithContext(ctx).Model(&Resource{}).Where(map[string]interface{}{
		"group":    s.storageGroupResource.Group,
		"version":  s.storageVersion.Version,
		"resource": s.storageGroupResource.Resource,
	})
	offset, amount, query := applyListOptionsToQuery(query, opts)

	var resources []Resource
	result := query.Find(&resources)
	if result.Error != nil {
		return InterpreError(s.storageGroupResource.String(), result.Error)
	}

	list, err := meta.ListAccessor(listObject)
	if err != nil {
		return InterpreError(s.storageGroupResource.String(), err)
	}

	if opts.WithContinue != nil && *opts.WithContinue {
		if int64(len(resources)) == opts.Limit {
			list.SetContinue(strconv.FormatInt(offset+opts.Limit, 10))
		}
	}

	if amount != nil {
		remain := *amount - offset - int64(len(resources))
		if remain < 0 {
			remain = 0
		}
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
	for _, resource := range resources {
		if err := appendListItem(v, resource.Object, s.codec, newItemFunc); err != nil {
			return InterpreError(s.storageGroupResource.String(), fmt.Errorf("need ptr to slice: %v", err))
		}
	}
	return nil
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return &storage.ResourceStorageConfig{
		Codec:                s.codec,
		StorageGroupResource: s.storageGroupResource,
		StorageVersion:       s.storageVersion,
		MemoryVersion:        s.memoryVersion,
	}
}
