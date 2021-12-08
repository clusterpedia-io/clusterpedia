package informer

import (
	"strconv"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apiserver/pkg/storage/etcd3"
	"k8s.io/client-go/tools/cache"
)

type ResourceVersionStorage struct {
	keyFunc cache.KeyFunc

	lastResourceVersion *uint64
	cacheStorage        cache.ThreadSafeStore
}

var _ cache.KeyListerGetter = &ResourceVersionStorage{}

func NewResourceVersionStorage(keyFunc cache.KeyFunc) *ResourceVersionStorage {
	var lastResourceVersion uint64
	storage := &ResourceVersionStorage{
		keyFunc:             keyFunc,
		lastResourceVersion: &lastResourceVersion,
		cacheStorage:        cache.NewThreadSafeStore(cache.Indexers{}, cache.Indices{}),
	}
	return storage
}

func (c *ResourceVersionStorage) LastResourceVersion() string {
	return strconv.FormatUint(atomic.LoadUint64(c.lastResourceVersion), 10)
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

	resourceversion := accessor.GetResourceVersion()
	rv, err := etcd3.Versioner.ParseResourceVersion(resourceversion)
	if err != nil {
		return err
	}
	atomic.CompareAndSwapUint64(c.lastResourceVersion, *c.lastResourceVersion, rv)
	c.cacheStorage.Add(key, resourceversion)
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

	resourceversion := accessor.GetResourceVersion()
	rv, err := etcd3.Versioner.ParseResourceVersion(resourceversion)
	if err != nil {
		return err
	}
	atomic.CompareAndSwapUint64(c.lastResourceVersion, *c.lastResourceVersion, rv)
	c.cacheStorage.Update(key, resourceversion)
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

func (c *ResourceVersionStorage) Get(obj interface{}) (string, bool, error) {
	key, err := c.keyFunc(obj)
	if err != nil {
		return "", false, cache.KeyError{Obj: obj, Err: err}
	}
	version, exists := c.cacheStorage.Get(key)
	if exists {
		return version.(string), exists, nil
	}
	return "", false, nil
}

func (c *ResourceVersionStorage) ListKeys() []string {
	return c.cacheStorage.ListKeys()
}

func (c *ResourceVersionStorage) GetByKey(key string) (item interface{}, exists bool, err error) {
	item, exists = c.cacheStorage.Get(key)
	return item, exists, nil
}

func (c *ResourceVersionStorage) Replace(versions map[string]interface{}) error {
	var lastResourceVersion uint64
	for _, version := range versions {
		rv, err := etcd3.Versioner.ParseResourceVersion(version.(string))
		if err != nil {
			// TODO(iceber): handle err
			continue
		}

		if rv > lastResourceVersion {
			lastResourceVersion = rv
		}
	}
	atomic.StoreUint64(c.lastResourceVersion, lastResourceVersion)
	c.cacheStorage.Replace(versions, "")
	return nil
}
