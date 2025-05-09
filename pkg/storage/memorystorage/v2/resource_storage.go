package memorystorage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type watchersMap map[int]*cacheWatcher

func (wm watchersMap) addWatcher(w *cacheWatcher, number int) {
	wm[number] = w
}

func (wm watchersMap) deleteWatcher(number int) {
	delete(wm, number)
}

func (wm watchersMap) terminateAll(done func(*cacheWatcher)) {
	for key, watcher := range wm {
		delete(wm, key)
		done(watcher)
	}
}

type namespacedName struct {
	namespace string
	name      string
}

type indexedWatchers struct {
	allWatchers map[namespacedName]watchersMap
}

func (i *indexedWatchers) addWatcher(w *cacheWatcher, number int, scope namespacedName) {
	scopedWatchers, ok := i.allWatchers[scope]
	if !ok {
		scopedWatchers = watchersMap{}
		i.allWatchers[scope] = scopedWatchers
	}
	scopedWatchers.addWatcher(w, number)
}

func (i *indexedWatchers) deleteWatcher(number int, scope namespacedName) {
	i.allWatchers[scope].deleteWatcher(number)
	if len(i.allWatchers[scope]) == 0 {
		delete(i.allWatchers, scope)
	}
}

type ResourceStorage struct {
	storageConfig *storage.ResourceStorageConfig

	keyFunc   func(runtime.Object) (string, error)
	storeLock sync.RWMutex
	stores    map[string]cache.Indexer

	cacheLock  sync.RWMutex
	capacity   int
	cache      []*watchCacheEvent
	startIndex int
	endIndex   int

	timer *time.Timer

	cond  *sync.Cond
	clock clock.Clock

	incoming chan watchCacheEvent

	watcherIdx  int
	watcherLock sync.RWMutex
	watchers    indexedWatchers

	dispatching     bool
	watchersBuffer  []*cacheWatcher
	blockedWatchers []*cacheWatcher
	watchersToStop  []*cacheWatcher

	stopCh chan struct{}
}

func (s *ResourceStorage) cleanCluster(cluster string) {
	// When cleaning up a cluster, support is needed to configure the time to send
	// the removal of all resources in the cluster or a cluster removal event.
	// If a cluster removal event is sent, then the client informer needs to be adapted.
}

func (s *ResourceStorage) RecordEvent(ctx context.Context, cluster string, event *corev1.Event) error {
	return nil
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return s.storageConfig
}

var accessor = meta.NewAccessor()

func (s *ResourceStorage) getOrCreateClusterStore(cluster string) cache.Indexer {
	s.storeLock.RLock()
	store := s.stores[cluster]
	s.storeLock.RUnlock()
	if store != nil {
		return store
	}

	s.storeLock.Lock()
	defer s.storeLock.Unlock()
	store = cache.NewIndexer(storeElementKey, nil)
	s.stores[cluster] = store
	return store
}

func (s *ResourceStorage) Create(ctx context.Context, cluster string, obj runtime.Object) error {
	store := s.getOrCreateClusterStore(cluster)
	return s.processObject(store, obj, watch.Added,
		func(o *storeElement) error { return store.Add(store) },
	)
}

func (s *ResourceStorage) Update(ctx context.Context, cluster string, obj runtime.Object) error {
	store := s.getOrCreateClusterStore(cluster)
	return s.processObject(store, obj, watch.Modified,
		func(o *storeElement) error { return store.Update(store) },
	)
}

func (s *ResourceStorage) ConvertDeletedObject(obj interface{}) (runobj runtime.Object, _ error) {
	return nil, nil
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object) error {
	store := s.getOrCreateClusterStore(cluster)
	return s.processObject(store, obj, watch.Added,
		func(o *storeElement) error { return store.Delete(store) },
	)
}

func (s *ResourceStorage) processObject(store cache.Store, obj runtime.Object, action watch.EventType, updateFunc func(*storeElement) error) error {
	rv, _ := accessor.ResourceVersion(obj)
	indexRV, memoryRV := ConvertResourceVersionToMemoryResourceVersion(rv)
	if err := accessor.SetResourceVersion(obj, memoryRV); err != nil {
		return err
	}

	key, err := s.keyFunc(obj)
	if err != nil {
		return err
	}
	elem := &storeElement{Key: key, Object: obj}
	elem.Labels, elem.Annotations, _, err = getObjectAttrs(obj)
	if err != nil {
		return err
	}

	wcEvent := &watchCacheEvent{
		Type:            action,
		Key:             key,
		Object:          elem.Object,
		ObjLabels:       elem.Labels,
		IndexRV:         indexRV,
		ResourceVersion: memoryRV,
		RecordTime:      s.clock.Now(),
	}

	previous, exists, err := store.GetByKey(key)
	if err != nil {
		return err
	}
	if exists {
		previousElem := previous.(*storeElement)
		wcEvent.PrevObject = previousElem.Object
		wcEvent.PrevObjLabels = previousElem.Labels
	}

	if err := func() error {
		s.cacheLock.Lock()
		defer s.cacheLock.Unlock()
		s.updateCache(wcEvent)
		return updateFunc(elem)
	}(); err != nil {
		return err
	}

	s.eventHandler(wcEvent)
	return nil
}

func (s *ResourceStorage) updateCache(event *watchCacheEvent) {
	s.resizeCacheLocked(event.RecordTime)
	if s.isCacheFullLocked() {
		// Cache is full - remove the oldest element.
		s.startIndex++
	}
	s.cache[s.endIndex%s.capacity] = event
	s.endIndex++
}

const blockTimeout = 3 * time.Second

func (s *ResourceStorage) waitUntilFreshAndBlock(ctx context.Context, rvIndex uint64, rv uint64) error {
	startTime := s.clock.Now()

	if rvIndex > 0 {
		go func() {
			<-s.clock.After(blockTimeout)
			s.cond.Broadcast()
		}()
	}

	for versioner.Load() < rvIndex {
		if s.clock.Since(startTime) >= blockTimeout {
			return nil
			// return storage.NewTooLargeResourceVersionError(resourceVersion, w.resourceVersion, resourceVersionTooHighRetrySeconds)
		}
		s.cond.Wait()
	}
	return nil
}

func (s *ResourceStorage) Get(ctx context.Context, cluster, namespace, name string, into runtime.Object) error {
	s.storeLock.RLock()
	store := s.stores[cluster]
	if store == nil {
		s.storeLock.RUnlock()
		return errors.New("not found")
	}
	s.storeLock.RUnlock()

	obj, exists, err := store.GetByKey(fmt.Sprintf("%s/%s", namespace, name))
	if err != nil {
		return err
	}
	objVal, err := conversion.EnforcePtr(into)
	if err != nil {
		return err
	}
	if !exists {
		objVal.Set(reflect.Zero(objVal.Type()))
		return errors.New("not found")
	}

	elem, ok := obj.(*storeElement)
	if !ok {
		return fmt.Errorf("non *storeElement returned from storage: %v", obj)
	}
	objVal.Set(reflect.ValueOf(elem.Object).Elem())
	return nil
}

func (s *ResourceStorage) List(ctx context.Context, listObject runtime.Object, opts *internal.ListOptions) error {
	var indexRV uint64
	var objs []runtime.Object
	for _, store := range s.stores {
		for _, obj := range store.List() {
			elem, ok := obj.(*storeElement)
			if !ok {
				return fmt.Errorf("non *storeElement returned from storage: %v", obj)
			}
			if elem.IndexRV > indexRV {
				indexRV = elem.IndexRV
			}
			// filter elem
			objs = append(objs, elem.Object.DeepCopyObject())
		}
	}

	listPtr, err := meta.GetItemsPtr(listObject)
	if err != nil {
		return err
	}
	listVal, err := conversion.EnforcePtr(listPtr)
	if err != nil {
		return err
	}
	if listVal.Kind() != reflect.Slice {
		return fmt.Errorf("need a pointer to slice, got %v", listVal.Kind())
	}
	listVal.Set(reflect.MakeSlice(listVal.Type(), len(objs), len(objs)))
	for i, o := range objs {
		listVal.Index(i).Set(reflect.ValueOf(o).Elem())
	}
	if err := accessor.SetResourceVersion(listObject, ConvertToMemoryResourceVersionForList(indexRV)); err != nil {
		return err
	}
	return nil
}

func (s *ResourceStorage) Watch(ctx context.Context, options *internal.ListOptions) (watch.Interface, error) {
	indexRV, err := ConvertMemoryResourceVersionToResourceVersionForList(options.ResourceVersion)
	if err != nil {
		return nil, err
	}

	scope := namespacedName{}
	watcher := newCacheWatcher(100, func(key string, _ labels.Set, f fields.Set) bool { return true })
	cacheInterval, err := s.getAllEventsSinceLocked(indexRV)
	if err != nil {
		return nil, errors.New("")
	}

	func() {
		s.watcherLock.Lock()
		defer s.watcherLock.Unlock()

		watcher.forget = forgetWatcher(s, watcher, s.watcherIdx, scope)
		s.watchers.addWatcher(watcher, s.watcherIdx, scope)
		s.watcherIdx++
	}()

	go watcher.processInterval(ctx, cacheInterval, indexRV)
	return watcher, nil
}

func (s *ResourceStorage) getAllEventsSinceLocked(indexRV uint64) (*watchCacheInterval, error) {
	size := s.endIndex - s.startIndex
	var oldest uint64
	switch {
	case size > 0:
		oldest = s.cache[s.startIndex%s.capacity].IndexRV
	default:
		return nil, errors.New("")
	}

	if indexRV == 0 {
		indexRV = versioner.Load()
	}
	if indexRV < oldest-1 {
		return nil, errors.New("")
	}

	f := func(i int) bool {
		return s.cache[(s.startIndex+i)%s.capacity].IndexRV > indexRV
	}
	first := sort.Search(size, f)
	indexerFunc := func(i int) *watchCacheEvent {
		return s.cache[i%s.capacity]
	}
	indexValidator := func(i int) bool {
		return i >= s.startIndex
	}
	ci := newCacheInterval(s.startIndex+first, s.endIndex, indexerFunc, indexValidator, s.watcherLock.RLocker())
	return ci, nil
}

func (s *ResourceStorage) eventHandler(event *watchCacheEvent) {
	s.incoming <- *event
}

func (s *ResourceStorage) dispatchEvents() {
	for {
		select {
		case event, ok := <-s.incoming:
			if !ok {
				return
			}
			s.dispatchEvent(&event)
		case <-s.stopCh:
			return
		}
	}
}

func (s *ResourceStorage) dispatchEvent(event *watchCacheEvent) {
	s.startDispatching(event)
	defer s.finishDispatching()

	wcEvent := *event
	event = &wcEvent

	s.blockedWatchers = s.blockedWatchers[:0]
	for _, watcher := range s.watchersBuffer {
		if !watcher.nonblockingAdd(event) {
			s.blockedWatchers = append(s.blockedWatchers, watcher)
		}
	}

	if len(s.blockedWatchers) > 0 {
		timeout := 50 * time.Millisecond
		s.timer.Reset(timeout)

		timer := s.timer
		for _, watcher := range s.blockedWatchers {
			if !watcher.add(event, timer) {
				timer = nil
			}
		}
		if timer != nil && !timer.Stop() {
			<-timer.C
		}
	}
}

func (s *ResourceStorage) startDispatching(event *watchCacheEvent) {
	s.watcherLock.Lock()
	defer s.watcherLock.Unlock()
	s.dispatching = true
	s.watchersBuffer = s.watchersBuffer[:0]

	namespace := event.ObjFields["metadata.namespace"]
	name := event.ObjFields["metadata.name"]
	if len(namespace) > 0 {
		if len(name) > 0 {
			for _, watcher := range s.watchers.allWatchers[namespacedName{namespace: namespace, name: name}] {
				s.watchersBuffer = append(s.watchersBuffer, watcher)
			}
		}
		for _, watcher := range s.watchers.allWatchers[namespacedName{namespace: namespace}] {
			s.watchersBuffer = append(s.watchersBuffer, watcher)
		}
	}
	if len(name) > 0 {
		for _, watcher := range s.watchers.allWatchers[namespacedName{name: name}] {
			s.watchersBuffer = append(s.watchersBuffer, watcher)
		}
	}

	for _, watcher := range s.watchers.allWatchers[namespacedName{}] {
		s.watchersBuffer = append(s.watchersBuffer, watcher)
	}
}

func (s *ResourceStorage) finishDispatching() {
	s.watcherLock.Lock()
	defer s.watcherLock.Unlock()
	s.dispatching = false
	for _, watcher := range s.watchersToStop {
		watcher.stopLocked()
	}

	s.watchersToStop = s.watchersToStop[:0]
}

func (s *ResourceStorage) stopWatcherLocked(watcher *cacheWatcher) {
	if s.dispatching {
		s.watchersToStop = append(s.watchersToStop, watcher)
	} else {
		watcher.stopLocked()
	}
}

func forgetWatcher(c *ResourceStorage, w *cacheWatcher, index int, scope namespacedName) func() {
	return func() {
		c.watcherLock.Lock()
		defer c.watcherLock.Unlock()
		c.watchers.deleteWatcher(index, scope)
		c.stopWatcherLocked(w)
	}
}

const (
	eventFreshDuration = 75 * time.Second

	lowerBoundCapacity = 100
	upperBoundCapacity = 100 * 1024
)

func (s *ResourceStorage) resizeCacheLocked(eventTime time.Time) {
	if s.isCacheFullLocked() && eventTime.Sub(s.cache[s.startIndex%s.capacity].RecordTime) < eventFreshDuration {
		capacity := min(s.capacity*2, upperBoundCapacity)
		if capacity > s.capacity {
			s.doCacheResizeLocked(capacity)
		}
		return
	}
	if s.isCacheFullLocked() && eventTime.Sub(s.cache[(s.endIndex-s.capacity/4)%s.capacity].RecordTime) > eventFreshDuration {
		capacity := max(s.capacity/2, lowerBoundCapacity)
		if capacity < s.capacity {
			s.doCacheResizeLocked(capacity)
		}
		return
	}
}

// isCacheFullLocked used to judge whether watchCacheEvent is full.
// Assumes that lock is already held for write.
func (s *ResourceStorage) isCacheFullLocked() bool {
	return s.endIndex == s.startIndex+s.capacity
}

// doCacheResizeLocked resize watchCache's event array with different capacity.
// Assumes that lock is already held for write.
func (s *ResourceStorage) doCacheResizeLocked(capacity int) {
	newCache := make([]*watchCacheEvent, capacity)
	if capacity < s.capacity {
		// adjust startIndex if cache capacity shrink.
		s.startIndex = s.endIndex - capacity
	}
	for i := s.startIndex; i < s.endIndex; i++ {
		newCache[i%capacity] = s.cache[i%s.capacity]
	}
	s.cache = newCache
	s.capacity = capacity
}

type storeElement struct {
	Key         string
	IndexRV     uint64
	Object      runtime.Object
	Labels      labels.Set
	Annotations labels.Set
}

func storeElementKey(obj interface{}) (string, error) {
	elem, ok := obj.(*storeElement)
	if !ok {
		return "", fmt.Errorf("not a storeElement: %v", obj)
	}
	return elem.Key, nil
}

func storeElementObject(obj interface{}) (runtime.Object, error) {
	elem, ok := obj.(*storeElement)
	if !ok {
		return nil, fmt.Errorf("not a storeElement: %v", obj)
	}
	return elem.Object, nil
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

func getObjectAttrs(obj runtime.Object) (labels.Set, labels.Set, fields.Set, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, nil, err
	}

	objLabels := accessor.GetLabels()
	if objLabels == nil {
		objLabels = make(map[string]string)
	}
	labelSet := labels.Set(objLabels)

	annotations := accessor.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotationSet := labels.Set(annotations)

	return labelSet, annotationSet, nil, nil
}

type watchCacheEvent struct {
	Type   watch.EventType
	Object runtime.Object

	Key             string
	IndexRV         uint64
	ResourceVersion string

	ObjLabels labels.Set
	ObjFields fields.Set

	PrevObject    runtime.Object
	PrevObjLabels labels.Set
	PrevObjFields fields.Set

	RecordTime time.Time
}
