package watchcache

import (
	"context"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// CacheWatcher implements watch.Interface
type CacheWatcher struct {
	input   chan *watch.Event
	result  chan watch.Event
	done    chan struct{}
	stopped bool
	forget  func()
}

func NewCacheWatcher(chanSize int) *CacheWatcher {
	return &CacheWatcher{
		input:   make(chan *watch.Event, chanSize),
		result:  make(chan watch.Event, chanSize),
		done:    make(chan struct{}),
		stopped: false,
		forget:  func() {},
	}
}

// ResultChan implements watch.Interface.
func (c *CacheWatcher) ResultChan() <-chan watch.Event {
	return c.result
}

// Stop implements watch.Interface.
func (c *CacheWatcher) Stop() {
	c.forget()
}

func (c *CacheWatcher) StopThreadUnsafe() {
	if !c.stopped {
		c.stopped = true
		close(c.done)
		close(c.input)
	}
}

func (c *CacheWatcher) NonblockingAdd(event *watch.Event) bool {
	select {
	case c.input <- event:
		return true
	default:
		return false
	}
}

// Nil timer means that add will not block (if it can't send event immediately, it will break the watcher)
func (c *CacheWatcher) Add(event *watch.Event, timer *time.Timer) bool {
	// Try to send the event immediately, without blocking.
	if c.NonblockingAdd(event) {
		return true
	}

	closeFunc := func() {
		// This means that we couldn't send event to that watcher.
		// Since we don't want to block on it infinitely,
		// we simply terminate it.
		//klog.V(1).Infof("Forcing watcher close due to unresponsiveness: %v", c.objectType.String())
		c.forget()
	}

	if timer == nil {
		closeFunc()
		return false
	}

	// OK, block sending, but only until timer fires.
	select {
	case c.input <- event:
		return true
	case <-timer.C:
		closeFunc()
		return false
	}
}

func (c *CacheWatcher) sendWatchCacheEvent(event *watch.Event) {
	//watchEvent := c.convertToWatchEvent(event)
	watchEvent := event
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

// Process send the events which stored in watchCache into the result channel,and select the event from input channel into result channel continuously.
func (c *CacheWatcher) Process(ctx context.Context, initEvents []*watch.Event) {
	defer utilruntime.HandleCrash()

	for _, event := range initEvents {
		c.sendWatchCacheEvent(event)
	}

	defer close(c.result)
	defer c.Stop()
	for {
		select {
		case event, ok := <-c.input:
			if !ok {
				return
			}
			c.sendWatchCacheEvent(event)
		case <-ctx.Done():
			return
		}
	}
}
