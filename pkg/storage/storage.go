package storage

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

type StorageFactory interface {
	// Currently only supports returning a union of verbs for all resources,
	// in the future it may be necessary to return verbs depending on different resources.
	GetSupportedRequestVerbs() []string

	PrepareCluster(cluster string) error

	GetResourceVersions(ctx context.Context, cluster string) (map[schema.GroupVersionResource]map[string]interface{}, error)
	GetCollectionResources(ctx context.Context) ([]*internal.CollectionResource, error)

	NewResourceStorage(config *ResourceStorageConfig, initEventCache bool) (ResourceStorage, error)
	NewCollectionResourceStorage(cr *internal.CollectionResource) (CollectionResourceStorage, error)

	CleanCluster(ctx context.Context, cluster string) error
	CleanClusterResource(ctx context.Context, cluster string, gvr schema.GroupVersionResource) error
}

type ResourceStorage interface {
	GetStorageConfig() *ResourceStorageConfig

	Get(ctx context.Context, cluster, namespace, name string, obj runtime.Object) error
	List(ctx context.Context, listObj runtime.Object, opts *internal.ListOptions) error
	Watch(ctx context.Context, newfunc func() runtime.Object, options *internal.ListOptions, gvk schema.GroupVersionKind) (watch.Interface, error)

	Create(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error
	Update(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error

	ConvertDeletedObject(obj interface{}) (runtime.Object, error)
	Delete(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error

	ProcessEvent(ctx context.Context, eventType watch.EventType, obj runtime.Object, cluster string) error
	GetObj(ctx context.Context, cluster, namespace, name string) (runtime.Object, error)
}

type CollectionResourceStorage interface {
	Get(ctx context.Context, opts *internal.ListOptions) (*internal.CollectionResource, error)
}

type ResourceStorageConfig struct {
	Namespaced bool

	GroupResource        schema.GroupResource
	StorageGroupResource schema.GroupResource

	MemoryVersion  schema.GroupVersion
	StorageVersion schema.GroupVersion

	Codec runtime.Codec

	NewFunc func() runtime.Object
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
