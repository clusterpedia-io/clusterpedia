package informer

import (
	"fmt"
	"reflect"
	"sync"
	"time"
	"unsafe"

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

	resourceVersionGetter ResourceVersionGetter
}

type ResourceVersionGetter interface {
	LastResourceVersion() string
}

func NewNamedController(name string, getter ResourceVersionGetter, config *cache.Config) cache.Controller {
	return &controller{
		name:   name,
		config: *config,

		resourceVersionGetter: getter,
	}
}

func (c *controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	go func() {
		<-stopCh
		c.config.Queue.Close()
	}()
	reflector := cache.NewNamedReflector(
		c.name,
		c.config.ListerWatcher,
		c.config.ObjectType,
		c.config.Queue,
		c.config.FullResyncPeriod,
	)
	reflector.ShouldResync = c.config.ShouldResync
	reflector.WatchListPageSize = c.config.WatchListPageSize

	c.setLastResourceVersionForReflector(reflector)

	c.reflectorMutex.Lock()
	c.reflector = reflector
	c.reflectorMutex.Unlock()

	var wg wait.Group
	wg.StartWithChannel(stopCh, reflector.Run)

	wait.Until(c.processLoop, time.Second, stopCh)
	wg.Wait()
}

func (c *controller) setLastResourceVersionForReflector(reflector *cache.Reflector) {
	if c.resourceVersionGetter == nil {
		return
	}

	rv := c.resourceVersionGetter.LastResourceVersion()
	if rv == "" || rv == "0" {
		return
	}
	rvValue := reflect.ValueOf(rv)

	field := reflect.ValueOf(reflector).Elem().FieldByName("lastSyncResourceVersion")
	value := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	if value.Kind() != rvValue.Kind() {
		panic(fmt.Sprintf("reflector.lastSyncResourceVersion's value kind is %v", value.Kind()))
	}
	value.Set(rvValue)
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
				c.config.Queue.AddIfNotPresent(obj)
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
