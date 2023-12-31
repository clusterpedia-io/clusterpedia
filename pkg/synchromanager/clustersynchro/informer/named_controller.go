package informer

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
)

type Config struct {
	// The queue for your objects - has to be a DeltaFIFO due to
	// assumptions in the implementation. Your Process() function
	// should accept the output of this Queue's Pop() method.
	cache.Queue

	// Something that can list and watch your objects.
	cache.ListerWatcher

	// Something that can process a popped Deltas.
	Process cache.ProcessFunc

	// ObjectType is an example object of the type this controller is
	// expected to handle.  Only the type needs to be right, except
	// that when that is `unstructured.Unstructured` the object's
	// `"apiVersion"` and `"kind"` must also be right.
	ObjectType runtime.Object

	// FullResyncPeriod is the period at which ShouldResync is considered.
	FullResyncPeriod time.Duration

	// ShouldResync is periodically used by the reflector to determine
	// whether to Resync the Queue. If ShouldResync is `nil` or
	// returns true, it means the reflector should proceed with the
	// resync.
	ShouldResync cache.ShouldResyncFunc

	// If true, when Process() returns an error, re-enqueue the object.
	// TODO: add interface to let you inject a delay/backoff or drop
	//       the object completely if desired. Pass the object in
	//       question to this interface as a parameter.  This is probably moot
	//       now that this functionality appears at a higher level.
	RetryOnError bool

	// Called whenever the ListAndWatch drops the connection with an error.
	WatchErrorHandler WatchErrorHandler

	// WatchListPageSize is the requested chunk size of initial and relist watch lists.
	WatchListPageSize int64

	// StreamHandle of paginated list, resources within a pager will be processed
	// as soon as possible instead of waiting until all resources are pulled before calling the ResourceHandler.
	StreamHandleForPaginatedList bool

	// Force paging, Reflector will sometimes use APIServer's cache,
	// even if paging is specified APIServer will return all resources for performance,
	// then it will skip Reflector's streaming memory optimization.
	ForcePaginatedList bool
}

type controller struct {
	name   string
	config Config

	reflectorMutex sync.RWMutex
	reflector      *Reflector
	queue          cache.Queue
}

func NewNamedController(name string, config *Config) cache.Controller {
	return &controller{
		name:   name,
		config: *config,
	}
}

func (c *controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	go func() {
		<-stopCh
		c.config.Queue.Close()
	}()

	r := NewNamedReflector(
		c.name,
		c.config.ListerWatcher,
		c.config.ObjectType,
		c.config.Queue,
		c.config.FullResyncPeriod,
	)
	if c.config.WatchErrorHandler != nil {
		r.watchErrorHandler = c.config.WatchErrorHandler
	}
	r.ShouldResync = c.config.ShouldResync
	r.WatchListPageSize = c.config.WatchListPageSize
	r.ForcePaginatedList = c.config.ForcePaginatedList
	r.StreamHandleForPaginatedList = c.config.StreamHandleForPaginatedList

	c.reflectorMutex.Lock()
	c.reflector = r
	c.reflectorMutex.Unlock()

	var wg wait.Group
	wg.StartWithChannel(stopCh, r.Run)

	wait.Until(c.processLoop, time.Second, stopCh)
	wg.Wait()
}

func (c *controller) processLoop() {
	for {
		obj, err := c.config.Queue.Pop(cache.PopProcessFunc(c.config.Process))
		if err != nil {
			if err == cache.ErrFIFOClosed {
				return
			}
			if c.config.RetryOnError {
				// This is the safe way to re-enqueue.
				_ = c.config.Queue.AddIfNotPresent(obj)
			}
		}
	}
}

func (c *controller) HasSynced() bool {
	c.reflectorMutex.RLock()
	defer c.reflectorMutex.RUnlock()

	if c.queue == nil {
		return false
	}
	return c.queue.HasSynced() && c.reflector.HasInitializedSynced()
}

func (c *controller) LastSyncResourceVersion() string {
	c.reflectorMutex.RLock()
	defer c.reflectorMutex.RUnlock()

	if c.reflector == nil {
		return ""
	}
	return c.reflector.LastSyncResourceVersion()
}
