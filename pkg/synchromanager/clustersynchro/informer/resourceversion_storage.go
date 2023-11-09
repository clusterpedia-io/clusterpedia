package informer

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
)

type StorageElement struct {
	Version   string
	Deleted   bool
	Published bool
	Name      string
	Namespace string
}

type ResourceVersionStorage struct {
	keyFunc cache.KeyFunc

	cacheStorage cache.ThreadSafeStore
}

var _ cache.KeyListerGetter = &ResourceVersionStorage{}

func NewResourceVersionStorage() *ResourceVersionStorage {
	return &ResourceVersionStorage{
		cacheStorage: cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
		keyFunc:      cache.DeletionHandlingMetaNamespaceKeyFunc,
	}
}

func (c *ResourceVersionStorage) Add(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return cache.KeyError{Obj: obj, Err: err}
	}
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	c.cacheStorage.Add(key, accessor.GetResourceVersion())
	return nil
}

func (c *ResourceVersionStorage) Update(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return cache.KeyError{Obj: obj, Err: err}
	}
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	c.cacheStorage.Update(key, accessor.GetResourceVersion())
	return nil
}

func (c *ResourceVersionStorage) Delete(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return cache.KeyError{Obj: obj, Err: err}
	}

	c.cacheStorage.Delete(key)
	return nil
}

func (c *ResourceVersionStorage) Get(obj interface{}) (*StorageElement, bool, error) {
	key, err := c.keyFunc(obj)
	if err != nil {
		return nil, false, cache.KeyError{Obj: obj, Err: err}
	}
	version, exists := c.cacheStorage.Get(key)
	if !exists {
		return nil, false, nil
	}

	var se StorageElement
	var ok bool
	if se, ok = version.(StorageElement); !ok {
		return nil, false, nil
	}

	if !se.Deleted {
		return &se, exists, nil
	}
	return nil, false, nil
}

func (c *ResourceVersionStorage) ListKeys() []string {
	return c.cacheStorage.ListKeys()
}

func (c *ResourceVersionStorage) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists = c.cacheStorage.Get(key)
	return item, exists, nil
}

func (c *ResourceVersionStorage) Replace(versions map[string]interface{}) error {
	c.cacheStorage.Replace(versions, "")
	return nil
}
