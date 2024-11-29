package memorystorage

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// cacheWatcher implements watch.Interface
type cacheWatcher struct {
	input  chan *watchCacheEvent
	result chan watch.Event

	filter filterWithAttrsFunc

	stopped bool
	done    chan struct{}
	forget  func()
}

type filterWithAttrsFunc func(key string, l labels.Set, f fields.Set) bool

func newCacheWatcher(chanSize int, filter filterWithAttrsFunc) *cacheWatcher {
	return &cacheWatcher{
		input:   make(chan *watchCacheEvent, chanSize),
		result:  make(chan watch.Event, chanSize),
		done:    make(chan struct{}),
		filter:  filter,
		forget:  func() {},
		stopped: false,
	}
}

// ResultChan implements watch.Interface.
func (c *cacheWatcher) ResultChan() <-chan watch.Event {
	return c.result
}

// Stop implements watch.Interface.
func (c *cacheWatcher) Stop() {
	c.forget()
}

func (c *cacheWatcher) stopLocked() {
	if !c.stopped {
		c.stopped = true
		close(c.done)
		close(c.input)
	}
}

func (c *cacheWatcher) nonblockingAdd(event *watchCacheEvent) bool {
	select {
	case c.input <- event:
		return true
	default:
		return false
	}
}

// Nil timer means that add will not block (if it can't send event immediately, it will break the watcher)
func (c *cacheWatcher) add(event *watchCacheEvent, timer *time.Timer) bool {
	// Try to send the event immediately, without blocking.
	if c.nonblockingAdd(event) {
		return true
	}

	closeFunc := func() {
		c.forget()
	}

	if timer == nil {
		closeFunc()
		return false
	}

	select {
	case c.input <- event:
		return true
	case <-timer.C:
		closeFunc()
		return false
	}
}

func (c *cacheWatcher) convertToWatchEvent(event *watchCacheEvent) *watch.Event {
	curObjPasses := event.Type != watch.Deleted && c.filter(event.Key, event.ObjLabels, event.ObjFields)
	var oldObjPasses bool
	if event.PrevObject != nil {
		oldObjPasses = c.filter(event.Key, event.PrevObjLabels, event.PrevObjFields)
	}
	if !curObjPasses && !oldObjPasses {
		return nil
	}

	switch {
	case curObjPasses && !oldObjPasses:
		return &watch.Event{Type: watch.Added, Object: event.Object.DeepCopyObject()}
	case curObjPasses && oldObjPasses:
		return &watch.Event{Type: watch.Modified, Object: event.Object.DeepCopyObject()}

	case !curObjPasses && oldObjPasses:
		oldObj := event.PrevObject.DeepCopyObject()
		return &watch.Event{Type: watch.Deleted, Object: oldObj}
	}
	return nil
}

func (c *cacheWatcher) sendWatchCacheEvent(event *watchCacheEvent) {
	watchEvent := c.convertToWatchEvent(event)
	if watchEvent == nil {
		// Watcher is not interested in that object.
		return
	}

	// We need to ensure that if we put event X to the c.result, all
	// previous events were already put into it before, no matter whether
	// c.done is close or not.
	// Thus we cannot simply select from c.done and c.result and this
	// would give us non-determinism.
	// At the same time, we don't want to block infinitely on putting
	// to c.result, when c.done is already closed.

	// This ensures that with c.done already close, we at most once go
	// into the next select after this. With that, no matter which
	// statement we choose there, we will deliver only consecutive
	// events.
	select {
	case <-c.done:
		return
	default:
	}

	select {
	case c.result <- *watchEvent:
	case <-c.done:
	}
}

func (c *cacheWatcher) processInterval(ctx context.Context, cacheInterval *watchCacheInterval, indexRV uint64) {
	defer utilruntime.HandleCrash()

	defer close(c.result)
	defer c.Stop()

	for {
		event, err := cacheInterval.Next()
		if err != nil {
			return
		}
		if event == nil {
			break
		}
		c.sendWatchCacheEvent(event)

		if event.IndexRV > indexRV {
			indexRV = event.IndexRV
		}
	}

	for {
		select {
		case event, ok := <-c.input:
			if !ok {
				return
			}
			if event.IndexRV > indexRV {
				c.sendWatchCacheEvent(event)
			}
		case <-ctx.Done():
			return
		}
	}
}
