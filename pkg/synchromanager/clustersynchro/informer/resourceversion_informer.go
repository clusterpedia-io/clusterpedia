package informer

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/tools/cache"
)

type ResourceVersionInformer interface {
	Run(stopCh <-chan struct{})
	HasSynced() bool
}

type resourceVersionInformer struct {
	name          string
	storage       *ResourceVersionStorage
	handler       ResourceEventHandler
	controller    cache.Controller
	listerWatcher cache.ListerWatcher
}

func NewResourceVersionInformer(name string, lw cache.ListerWatcher, storage *ResourceVersionStorage, exampleObject runtime.Object, handler ResourceEventHandler, errorHandler WatchErrorHandler, extraStore ExtraStore) ResourceVersionInformer {
	if name == "" {
		panic("name is required")
	}

	// storage: NewResourceVersionStorage(cache.DeletionHandlingMetaNamespaceKeyFunc),
	informer := &resourceVersionInformer{
		name:          name,
		listerWatcher: lw,
		storage:       storage,
		handler:       handler,
	}

	var queue cache.Queue = cache.NewDeltaFIFOWithOptions(cache.DeltaFIFOOptions{
		KeyFunction:           cache.DeletionHandlingMetaNamespaceKeyFunc,
		KnownObjects:          informer.storage,
		EmitDeltaTypeReplaced: true,
	})
	if extraStore != nil {
		queue = &queueWithExtraStore{Queue: queue, extra: extraStore}
	}

	config := &Config{
		ListerWatcher: lw,
		ObjectType:    exampleObject,
		RetryOnError:  false,
		Process: func(obj interface{}, isInInitialList bool) error {
			deltas := obj.(cache.Deltas)
			return informer.HandleDeltas(deltas, isInInitialList)
		},
		Queue:             queue,
		WatchErrorHandler: errorHandler,
	}
	informer.controller = NewNamedController(informer.name, config)
	return informer
}

func (informer *resourceVersionInformer) HasSynced() bool {
	return informer.controller.HasSynced()
}

func (informer *resourceVersionInformer) Run(stopCh <-chan struct{}) {
	informer.controller.Run(stopCh)
}

func (informer *resourceVersionInformer) HandleDeltas(deltas cache.Deltas, isInInitialList bool) error {
	for _, d := range deltas {
		switch d.Type {
		case cache.Replaced, cache.Added, cache.Updated:
			version, exists, err := informer.storage.Get(d.Object)
			if err != nil {
				return err
			}

			if !exists {
				if err := informer.storage.Add(d.Object); err != nil {
					return err
				}

				informer.handler.OnAdd(d.Object, isInInitialList)
				break
			}

			if d.Type == cache.Replaced {
				if v := compareResourceVersion(d.Object, version); v <= 0 {
					if v == 0 {
						informer.handler.OnSync(d.Object)
					}
					break
				}
			}

			if err := informer.storage.Update(d.Object); err != nil {
				return err
			}
			informer.handler.OnUpdate(nil, d.Object)
		case cache.Deleted:
			if err := informer.storage.Delete(d.Object); err != nil {
				return err
			}
			informer.handler.OnDelete(d.Object)
		}
	}
	return nil
}

var versioner storage.Versioner = storage.APIObjectVersioner{}

func compareResourceVersion(obj interface{}, rv string) int {
	object, ok := obj.(runtime.Object)
	if !ok {
		// TODO(clusterpedia-io): add log
		return -1
	}

	objversion, err := versioner.ObjectResourceVersion(object)
	if err != nil {
		return -1
	}

	version, err := versioner.ParseResourceVersion(rv)
	if err != nil {
		return -1
	}

	if objversion == version {
		return 0
	}
	if objversion < version {
		return -1
	}
	return 1
}
