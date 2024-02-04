package watchcache

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/watch"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/strings/slices"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
	utilwatch "github.com/clusterpedia-io/clusterpedia/pkg/utils/watch"
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

	// ResourceVersion up to which the watchCache is propagated.
	resourceVersion *ClusterResourceVersion

	//eventHandler func(*watchCacheEvent)

	WatchersLock sync.RWMutex

	// watchersBuffer is a list of watchers potentially interested in currently
	// dispatched event.
	WatchersBuffer []*CacheWatcher
	// blockedWatchers is a list of watchers whose buffer is currently full.
	BlockedWatchers []*CacheWatcher
	IsNamespaced    bool
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

func GetAttrsFunc(obj runtime.Object) (labels.Set, fields.Set, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}
	objLabels := accessor.GetLabels()
	if objLabels == nil {
		objLabels = make(map[string]string)
	}
	labelSet := labels.Set(objLabels)
	return labelSet, nil, nil
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

func NewWatchCache(capacity int, gvr schema.GroupVersionResource, isNamespaced bool) *WatchCache {
	wc := &WatchCache{
		capacity:     capacity,
		KeyFunc:      GetKeyFunc(gvr, isNamespaced),
		getAttrsFunc: GetAttrsFunc,
		cache:        make([]*watch.Event, capacity),
		startIndex:   0,
		endIndex:     0,
		stores:       make(map[string]cache.Indexer),
		IsNamespaced: isNamespaced,
	}

	return wc
}

func (w *WatchCache) GetStores() map[string]cache.Indexer {
	return w.stores
}

func (w *WatchCache) processEvent(event watch.Event, resourceVersion *ClusterResourceVersion, updateFunc func(*StoreElement) error) error {
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

	wcEvent := event

	if err := func() error {
		// TODO: We should consider moving this lock below after the watchCacheEvent
		// is created. In such situation, the only problematic scenario is Replace(
		// happening after getting object from store and before acquiring a lock.
		// Maybe introduce another lock for this purpose.
		w.Lock()
		defer w.Unlock()

		w.updateCache(&wcEvent)
		w.resourceVersion = resourceVersion
		return updateFunc(elem)
	}(); err != nil {
		return err
	}

	return nil
}

// Add takes runtime.Object as an argument.
func (w *WatchCache) Add(obj runtime.Object, clusterName string, resourceVersion *ClusterResourceVersion,
	codec runtime.Codec, memoryVersion schema.GroupVersion) error {
	f := func(elem *StoreElement) error {
		return w.stores[clusterName].Add(elem)
	}
	object, err := encodeEvent(obj, codec, memoryVersion)
	if err != nil {
		return err
	}

	event := watch.Event{Type: watch.Added, Object: object}
	err = w.processEvent(event, resourceVersion, f)
	if err != nil {
		return err
	}

	w.dispatchEvent(&event)
	return nil
}

// Update takes runtime.Object as an argument.
func (w *WatchCache) Update(obj runtime.Object, clusterName string, resourceVersion *ClusterResourceVersion,
	codec runtime.Codec, memoryVersion schema.GroupVersion) error {
	f := func(elem *StoreElement) error {
		return w.stores[clusterName].Update(elem)
	}
	object, err := encodeEvent(obj, codec, memoryVersion)
	if err != nil {
		return err
	}

	event := watch.Event{Type: watch.Modified, Object: object}
	err = w.processEvent(event, resourceVersion, f)
	if err != nil {
		return err
	}

	w.dispatchEvent(&event)
	return nil
}

// Delete takes runtime.Object as an argument.
func (w *WatchCache) Delete(obj runtime.Object, clusterName string, resourceVersion *ClusterResourceVersion,
	codec runtime.Codec, memoryVersion schema.GroupVersion) error {
	f := func(elem *StoreElement) error {
		return w.stores[clusterName].Delete(elem)
	}
	object, err := encodeEvent(obj, codec, memoryVersion)
	if err != nil {
		return err
	}

	event := watch.Event{Type: watch.Deleted, Object: object}
	err = w.processEvent(event, resourceVersion, f)
	if err != nil {
		return err
	}

	w.dispatchEvent(&event)
	return nil
}

// Assumes that lock is already held for write.
func (w *WatchCache) updateCache(event *watch.Event) {
	if w.endIndex == w.startIndex+w.capacity {
		// Cache is full - remove the oldest element.
		w.startIndex++
	}
	w.cache[w.endIndex%w.capacity] = event
	w.endIndex++
}

// WaitUntilFreshAndList returns list of pointers to <storeElement> objects.
func (w *WatchCache) WaitUntilFreshAndList(opts *internal.ListOptions) ([]*StoreElement, *ClusterResourceVersion, error) {
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
	searchAllCluster := true
	searchClusters := sets.NewString()
	if opts != nil && len(opts.ClusterNames) != 0 {
		for _, tmp := range opts.ClusterNames {
			searchClusters.Insert(tmp)
		}
		searchAllCluster = false
	}
	for cluster, store := range w.stores {
		if !searchAllCluster && !searchClusters.Has(cluster) {
			continue
		}
		for _, obj := range store.List() {
			se := obj.(*StoreElement)
			ns, err := accessor.Namespace(se.Object)
			if err != nil {
				return result, w.resourceVersion, err
			}
			if opts.Namespaces != nil {
				if slices.Contains(opts.Namespaces, ns) {
					result = append(result, se)
					continue
				} else {
					continue
				}
			}
			if filterLabelSelector(opts.LabelSelector, se.Labels) {
				continue
			}
			result = append(result, se)
		}
	}
	return result, w.resourceVersion, nil
}

// filterLabelSelector returns true if the given label selector matches the given label set. else false.
func filterLabelSelector(labelSelector labels.Selector, label labels.Set) bool {
	if labelSelector != nil && label == nil {
		return true
	}
	if labelSelector != nil && label != nil && !labelSelector.Matches(label) {
		return true
	}
	return false
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

// GetAllEventsSinceThreadUnsafe returns watch event from slice window in watchCache by the resourceVersion
func (w *WatchCache) GetAllEventsSinceThreadUnsafe(resourceVersion *ClusterResourceVersion) ([]*watch.Event, error) {
	size := w.endIndex - w.startIndex
	if resourceVersion.IsEmpty() {
		// resourceVersion = 0 means that we don't require any specific starting point
		// and we would like to start watching from ~now.
		// However, to keep backward compatibility, we additionally need to return the
		// current state and only then start watching from that point.
		//
		// TODO: In v2 api, we should stop returning the current state - #13969.
		var allItems []interface{}
		for _, store := range w.stores {
			allItems = append(allItems, store.List()...)
		}
		result := make([]*watch.Event, len(allItems))
		for i, item := range allItems {
			elem, ok := item.(*StoreElement)
			if !ok {
				return nil, fmt.Errorf("not a storeElement: %v", elem)
			}
			result[i] = &watch.Event{
				Type:   watch.Added,
				Object: elem.Object,
			}
		}
		return result, nil
	}

	var index int
	var founded bool
	for index = 0; w.startIndex+index < w.endIndex; index++ {
		rv, err := GetClusterResourceVersionFromEvent(w.cache[(w.startIndex+index)%w.capacity])
		if err != nil {
			return nil, err
		}
		if rv.IsEqual(resourceVersion) {
			founded = true
			index++
			break
		}
	}

	var result []*watch.Event
	if !founded {
		return nil, apierrors.NewResourceExpired("resource version not found")
	} else {
		i := 0
		result = make([]*watch.Event, size-index)
		for ; w.startIndex+index < w.endIndex; index++ {
			result[i] = w.cache[(w.startIndex+index)%w.capacity]
			i++
		}
	}

	return result, nil
}

func (w *WatchCache) AddIndexer(clusterName string, indexers *cache.Indexers) {
	w.Lock()
	defer w.Unlock()

	if _, ok := w.stores[clusterName]; !ok {
		w.stores[clusterName] = cache.NewIndexer(storeElementKey, storeElementIndexers(indexers))
	}
}

func (w *WatchCache) DeleteIndexer(clusterName string) bool {
	w.Lock()
	defer w.Unlock()

	if _, ok := w.stores[clusterName]; !ok {
		return false
	}

	delete(w.stores, clusterName)

	//clear cache
	w.startIndex = 0
	w.endIndex = 0

	return true
}

func (w *WatchCache) dispatchEvent(event *watch.Event) {
	w.WatchersLock.RLock()
	defer w.WatchersLock.RUnlock()
	for _, watcher := range w.WatchersBuffer {
		watcher.NonblockingAdd(event)
	}
}

func (w *WatchCache) CleanCluster(cluster string) {
	if !w.DeleteIndexer(cluster) {
		return
	}

	errorEvent := utilwatch.NewErrorEvent(fmt.Errorf("cluster %s has been clean", cluster))
	w.dispatchEvent(&errorEvent)
}

func encodeEvent(obj runtime.Object, codec runtime.Codec, memoryVersion schema.GroupVersion) (runtime.Object, error) {
	var buffer bytes.Buffer

	//gvk := obj.GetObjectKind().GroupVersionKind()
	gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
	if ok := scheme.LegacyResourceScheme.IsGroupRegistered(gk.Group); !ok {
		return obj, nil
	}

	dest, err := scheme.LegacyResourceScheme.New(memoryVersion.WithKind(gk.Kind))
	if err != nil {
		return nil, err
	}

	err = codec.Encode(obj, &buffer)
	if err != nil {
		return nil, err
	}

	object, _, err := codec.Decode(buffer.Bytes(), nil, dest)
	if err != nil {
		return nil, err
	}

	if object != dest {
		return nil, err
	}

	return dest, nil
}
