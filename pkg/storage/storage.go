package storage

import (
	"context"

	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

type StorageFactory interface {
	GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error)
	PrepareCluster(cluster string) error
	CleanCluster(ctx context.Context, cluster string) error
	CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error

	NewResourceStorage(config *ResourceStorageConfig) (ResourceStorage, error)
	NewCollectionResourceStorage(cr *internal.CollectionResource) (CollectionResourceStorage, error)

	GetCollectionResources(ctx context.Context) ([]*internal.CollectionResource, error)
}

type ResourceStorage interface {
	GetStorageConfig() *ResourceStorageConfig

	Get(ctx context.Context, cluster, namespace, name string, obj runtime.Object) error
	List(ctx context.Context, listObj runtime.Object, opts *internal.ListOptions) error
	Watch(ctx context.Context, options *metainternalversion.ListOptions) (watch.Interface, error)

	Create(ctx context.Context, cluster string, obj runtime.Object) error
	Update(ctx context.Context, cluster string, obj runtime.Object) error
	Delete(ctx context.Context, cluster string, obj runtime.Object) error
}

type CollectionResourceStorage interface {
	Get(ctx context.Context, opts *internal.ListOptions) (*internal.CollectionResource, error)
}

type ResourceStorageConfig struct {
	GroupResource        schema.GroupResource
	StorageGroupResource schema.GroupResource

	Codec          runtime.Codec
	StorageVersion schema.GroupVersion
	MemoryVersion  schema.GroupVersion
	Namespaced     bool
}

type storageRecoverableExceptionError struct {
	error
}

func NewRecoverableException(err error) error {
	return storageRecoverableExceptionError{err}
}

func IsRecoverableException(err error) bool {
	_, ok := err.(storageRecoverableExceptionError)
	return ok
}
