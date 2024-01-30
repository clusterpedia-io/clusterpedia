package components

import (
	"strings"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
)

var cacheSize = 100

var (
	eventCachePool *EventCachePool
	one            sync.Once
)

type EventCachePool struct {
	lock   sync.Mutex
	cache  map[schema.GroupVersionResource]*EventCache
	stopCh <-chan struct{}
}

type EventCache struct {
	lock sync.RWMutex
	// watcher               watch.Interface
	cache []*watch.Event
	gvr   schema.GroupVersionResource
	// cache数组范围 循环利用
	startIndex int
	endIndex   int
}

func newEventCache(gvr schema.GroupVersionResource) *EventCache {
	return &EventCache{
		cache:      make([]*watch.Event, cacheSize),
		gvr:        gvr,
		startIndex: 0,
		endIndex:   0,
	}
}

func InitEventCacheSize(cs int) {
	cacheSize = cs
}

func InitEventCachePool(stopCh <-chan struct{}) *EventCachePool {
	one.Do(func() {
		eventCachePool = &EventCachePool{
			cache:  map[schema.GroupVersionResource]*EventCache{},
			stopCh: stopCh,
		}
	})
	return eventCachePool
}

func GetInitEventCachePool() *EventCachePool {
	return eventCachePool
}

func (e *EventCachePool) GetWatchEventCacheByGVR(gvr schema.GroupVersionResource) *EventCache {
	e.lock.Lock()
	defer e.lock.Unlock()
	if watchcache, ok := e.cache[gvr]; ok {
		return watchcache
	} else {
		ec := newEventCache(gvr)
		e.cache[gvr] = ec
		return ec
	}
}

func (e *EventCachePool) ClearCacheByGVR(gvr schema.GroupVersionResource) {
	e.lock.Lock()
	defer e.lock.Unlock()
	ec := e.cache[gvr]
	if ec != nil {
		ec.Clear()
	}
}

func (e *EventCache) ExistsInCache(resourceVersion string) bool {
	e.lock.RLock()
	defer e.lock.RUnlock()

	accessor := meta.NewAccessor()
	var index int
	for index = 0; e.startIndex+index < e.endIndex; index++ {
		value := e.cache[(e.startIndex+index)%cacheSize]
		rv, _ := accessor.ResourceVersion(value.Object)
		if strings.Compare(resourceVersion, rv) == 0 {
			return true
		}
	}

	return false
}

// GetEvents returns newer events by comparing crv
func (e *EventCache) GetEvents(crv string, getMaxCrv func() (string, error)) ([]*watch.Event, error) {
	e.lock.RLock()
	defer e.lock.RUnlock()
	// Judge whether the RV is the latest when cache is empty and watch with RV.
	if e.startIndex == e.endIndex {
		maxCrv, err := getMaxCrv()
		if err != nil {
			return nil, err
		}
		if utils.IsEqual(maxCrv, crv) {
			return make([]*watch.Event, 0), nil
		} else {
			return nil, apierrors.NewResourceExpired("resource version not found")
		}
	} else {
		var found bool
		accessor := meta.NewAccessor()
		var index int
		var start int
		result := []*watch.Event{}
		for index = 0; e.startIndex+index < e.endIndex; index++ {
			value := e.cache[(e.startIndex+index)%cacheSize]
			rv, err := accessor.ResourceVersion(value.Object)
			if err != nil {
				return nil, err
			}
			// Unlikely that there will be a situation greater than
			if utils.IsEqual(rv, crv) {
				found = true
				start = e.startIndex + index
				break
			}
		}

		if found {
			i := 0
			resultSize := (e.endIndex - start) % cacheSize
			// append item to splice which is after the equal one
			for ; i < resultSize; i++ {
				result = append(result, e.cache[(start+i)%cacheSize])
			}
			return result, nil
		}
	}

	return nil, apierrors.NewResourceExpired("resource version not found")
}

func (e *EventCache) Enqueue(event *watch.Event) {
	e.lock.RLock()
	defer e.lock.RUnlock()

	if e.endIndex == e.startIndex+cacheSize {
		// Cache is full - remove the oldest element.
		e.startIndex++
	}
	e.cache[e.endIndex%cacheSize] = event
	e.endIndex++
}

func (e *EventCache) Clear() {
	e.lock.RLock()
	defer e.lock.RUnlock()

	e.startIndex = 0
	e.endIndex = 0
}
