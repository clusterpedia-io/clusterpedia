package informer

import (
	"sync"
	"time"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
)

type controller struct {
	name   string
	config cache.Config

	reflectorMutex sync.RWMutex
	reflector      *cache.Reflector
	queue          cache.Queue
}

func NewNamedController(name string, config *cache.Config) cache.Controller {
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
	r := cache.NewNamedReflector(
		c.name,
		c.config.ListerWatcher,
		c.config.ObjectType,
		c.config.Queue,
		c.config.FullResyncPeriod,
	)
	r.ShouldResync = c.config.ShouldResync
	r.WatchListPageSize = c.config.WatchListPageSize

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
	return c.queue.HasSynced()
}

func (c *controller) LastSyncResourceVersion() string {
	c.reflectorMutex.RLock()
	defer c.reflectorMutex.RUnlock()

	if c.reflector == nil {
		return ""
	}
	return c.reflector.LastSyncResourceVersion()
}
