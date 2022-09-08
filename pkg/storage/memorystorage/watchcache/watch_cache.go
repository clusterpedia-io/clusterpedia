package watchcache

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/strings/slices"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

// StoreElement keeping the structs of resource in k8s(key, object, labels, fields).
type StoreElement struct {
	Key    string
	Object runtime.Object
	Labels labels.Set
	Fields fields.Set
}

// WatchCache implements a Store interface.
// However, it depends on the elements implementing runtime.Object interface.
//
// WatchCache is a "sliding window" (with a limited capacity) of objects
// observed from a watch.
type WatchCache struct {
	sync.RWMutex

	// Maximum size of history window.
	capacity int

	// KeyFunc is used to get a key in the underlying storage for a given object.
	KeyFunc func(runtime.Object) (string, error)

	// getAttrsFunc is used to get labels and fields of an object.
	getAttrsFunc func(runtime.Object) (labels.Set, fields.Set, error)

	// cache is used a cyclic buffer - its first element (with the smallest
	// resourceVersion) is defined by startIndex, its last element is defined
	// by endIndex (if cache is full it will be startIndex + capacity).
	// Both startIndex and endIndex can be greater than buffer capacity -
	// you should always apply modulo capacity to get an index in cache array.
	cache      []*watch.Event
	startIndex int
	endIndex   int

	// store will effectively support LIST operation from the "end of cache
	// history" i.e. from the moment just after the newest cached watched event.
	// It is necessary to effectively allow clients to start watching at now.
	// NOTE: We assume that <store> is thread-safe.
	stores map[string]cache.Indexer

	//eventHandler func(*watchCacheEvent)

	WatchersLock sync.RWMutex

	IsNamespaced bool
}

type keyFunc func(runtime.Object) (string, error)

func GetKeyFunc(gvr schema.GroupVersionResource, isNamespaced bool) keyFunc {
	prefix := gvr.Group + "/" + gvr.Resource

	var KeyFunc func(ctx context.Context, name string) (string, error)
	if isNamespaced {
		KeyFunc = func(ctx context.Context, name string) (string, error) {
			return registry.NamespaceKeyFunc(ctx, prefix, name)
		}
	} else {
		KeyFunc = func(ctx context.Context, name string) (string, error) {
			return registry.NoNamespaceKeyFunc(ctx, prefix, name)
		}
	}

	// We adapt the store's keyFunc so that we can use it with the StorageDecorator
	// without making any assumptions about where objects are stored in etcd
	kc := func(obj runtime.Object) (string, error) {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return "", err
		}

		if isNamespaced {
			return KeyFunc(genericapirequest.WithNamespace(genericapirequest.NewContext(), accessor.GetNamespace()), accessor.GetName())
		}

		return KeyFunc(genericapirequest.NewContext(), accessor.GetName())
	}

	return kc
}

func storeElementKey(obj interface{}) (string, error) {
	elem, ok := obj.(*StoreElement)
	if !ok {
		return "", fmt.Errorf("not a storeElement: %v", obj)
	}
	return elem.Key, nil
}

func storeElementIndexers(indexers *cache.Indexers) cache.Indexers {
	if indexers == nil {
		return cache.Indexers{}
	}
	ret := cache.Indexers{}
	for indexName, indexFunc := range *indexers {
		ret[indexName] = storeElementIndexFunc(indexFunc)
	}
	return ret
}

func storeElementIndexFunc(objIndexFunc cache.IndexFunc) cache.IndexFunc {
	return func(obj interface{}) (strings []string, e error) {
		seo, err := storeElementObject(obj)
		if err != nil {
			return nil, err
		}
		return objIndexFunc(seo)
	}
}

func storeElementObject(obj interface{}) (runtime.Object, error) {
	elem, ok := obj.(*StoreElement)
	if !ok {
		return nil, fmt.Errorf("not a storeElement: %v", obj)
	}
	return elem.Object, nil
}

func NewWatchCache(capacity int, gvr schema.GroupVersionResource, isNamespaced bool, newCluster string) *WatchCache {
	wc := &WatchCache{
		capacity:     capacity,
		KeyFunc:      GetKeyFunc(gvr, isNamespaced),
		getAttrsFunc: nil,
		cache:        make([]*watch.Event, capacity),
		startIndex:   0,
		endIndex:     0,
		stores:       make(map[string]cache.Indexer),
		IsNamespaced: isNamespaced,
	}

	wc.AddIndexer(newCluster, nil)
	return wc
}

func (w *WatchCache) GetStores() map[string]cache.Indexer {
	return w.stores
}

func (w *WatchCache) processEvent(event watch.Event, updateFunc func(*StoreElement) error) error {
	key, err := w.KeyFunc(event.Object)
	if err != nil {
		return fmt.Errorf("couldn't compute key: %v", err)
	}
	elem := &StoreElement{Key: key, Object: event.Object}
	if w.getAttrsFunc != nil {
		elem.Labels, elem.Fields, err = w.getAttrsFunc(event.Object)
		if err != nil {
			return err
		}
	}

	if err := func() error {
		// TODO: We should consider moving this lock below after the watchCacheEvent
		// is created. In such situation, the only problematic scenario is Replace(
		// happening after getting object from store and before acquiring a lock.
		// Maybe introduce another lock for this purpose.
		w.Lock()
		defer w.Unlock()

		return updateFunc(elem)
	}(); err != nil {
		return err
	}

	return nil
}

// Add takes runtime.Object as an argument.
func (w *WatchCache) Add(obj interface{}, clusterName string) error {
	event := watch.Event{Type: watch.Added, Object: obj.(runtime.Object)}

	f := func(elem *StoreElement) error {
		return w.stores[clusterName].Add(elem)
	}
	return w.processEvent(event, f)
}

// Update takes runtime.Object as an argument.
func (w *WatchCache) Update(obj interface{}, clusterName string) error {
	event := watch.Event{Type: watch.Modified, Object: obj.(runtime.Object)}

	f := func(elem *StoreElement) error {
		return w.stores[clusterName].Update(elem)
	}
	return w.processEvent(event, f)
}

// Delete takes runtime.Object as an argument.
func (w *WatchCache) Delete(obj interface{}, clusterName string) error {
	event := watch.Event{Type: watch.Deleted, Object: obj.(runtime.Object)}

	f := func(elem *StoreElement) error { return w.stores[clusterName].Delete(elem) }
	return w.processEvent(event, f)
}

// WaitUntilFreshAndList returns list of pointers to <storeElement> objects.
func (w *WatchCache) WaitUntilFreshAndList(opts *internal.ListOptions) []*StoreElement {
	w.RLock()
	defer w.RUnlock()

	/*	// This isn't the place where we do "final filtering" - only some "prefiltering" is happening here. So the only
		// requirement here is to NOT miss anything that should be returned. We can return as many non-matching items as we
		// want - they will be filtered out later. The fact that we return less things is only further performance improvement.
		// TODO: if multiple indexes match, return the one with the fewest items, so as to do as much filtering as possible.
		for _, matchValue := range matchValues {
			if result, err := w.store.ByIndex(matchValue.IndexName, matchValue.Value); err == nil {
				return result, w.resourceVersion, nil
			}
		}*/
	var result []*StoreElement
	accessor := meta.NewAccessor()
	for _, store := range w.stores {
		for _, obj := range store.List() {
			se := obj.(*StoreElement)
			ns, _ := accessor.Namespace(se.Object)

			if opts.Namespaces != nil {
				if slices.Contains(opts.Namespaces, ns) {
					result = append(result, se)
					continue
				} else {
					continue
				}
			}
			result = append(result, se)
		}
	}
	return result
}

// WaitUntilFreshAndGet returns list of pointers to <storeElement> objects.
func (w *WatchCache) WaitUntilFreshAndGet(cluster, namespace, name string) (*StoreElement, error) {
	w.RLock()
	defer w.RUnlock()

	var result *StoreElement
	accessor := meta.NewAccessor()
	if _, ok := w.stores[cluster]; !ok {
		return nil, fmt.Errorf("cluster %s is not existed", cluster)
	}

	for _, obj := range w.stores[cluster].List() {
		se := obj.(*StoreElement)
		ns, err := accessor.Namespace(se.Object)
		if err != nil {
			return result, err
		}
		if namespace != "" && namespace != ns {
			continue
		}
		n, err := accessor.Name(se.Object)
		if err != nil {
			return result, err
		}
		if name != "" {
			if ns == namespace && n == name {
				return se, nil
			} else {
				continue
			}
		}
	}

	return result, nil
}

func (w *WatchCache) AddIndexer(clusterName string, indexers *cache.Indexers) {
	w.Lock()
	defer w.Unlock()
	w.stores[clusterName] = cache.NewIndexer(storeElementKey, storeElementIndexers(indexers))
}

func (w *WatchCache) DeleteIndexer(clusterName string) {
	w.Lock()
	defer w.Unlock()

	if _, ok := w.stores[clusterName]; !ok {
		return
	}

	delete(w.stores, clusterName)

	//clear cache
	w.startIndex = 0
	w.endIndex = 0
}

func (w *WatchCache) ClearWatchCache(clusterName string) {
	w.Lock()
	defer w.Unlock()

	if _, ok := w.stores[clusterName]; !ok {
		return
	}

	delete(w.stores, clusterName)
	w.stores[clusterName] = cache.NewIndexer(storeElementKey, storeElementIndexers(nil))

	//clear cache
	w.startIndex = 0
	w.endIndex = 0
}
