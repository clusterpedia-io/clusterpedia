package informer

import (
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

func NewInformer(
	lw cache.ListerWatcher,
	objType runtime.Object,
	store cache.Store,
	resyncPeriod time.Duration,
	h cache.ResourceEventHandler,
	transfor cache.TransformFunc,
) cache.Controller {
	return newInformer(lw, objType, resyncPeriod, h, store, transfor)
}

// Multiplexes updates in the form of a list of Deltas into a Store, and informs
// a given handler of events OnUpdate, OnAdd, OnDelete
func processDeltas(
	// Object which receives event notifications from the given deltas
	handler cache.ResourceEventHandler,
	clientState cache.Store,
	transformer cache.TransformFunc,
	deltas cache.Deltas,
	isInInitialList bool,
) error {
	// from oldest to newest
	for _, d := range deltas {
		obj := d.Object
		if transformer != nil {
			var err error
			obj, err = transformer(obj)
			if err != nil {
				return err
			}
		}

		switch d.Type {
		case cache.Sync, cache.Replaced, cache.Added, cache.Updated:
			if old, exists, err := clientState.Get(obj); err == nil && exists {
				if err := clientState.Update(obj); err != nil {
					return err
				}
				handler.OnUpdate(old, obj)
			} else {
				if err := clientState.Add(obj); err != nil {
					return err
				}
				handler.OnAdd(obj, isInInitialList)
			}
		case cache.Deleted:
			if err := clientState.Delete(obj); err != nil {
				return err
			}
			handler.OnDelete(obj)
		}
	}
	return nil
}

// newInformer returns a controller for populating the store while also
// providing event notifications.
//
// Parameters
//   - lw is list and watch functions for the source of the resource you want to
//     be informed of.
//   - objType is an object of the type that you expect to receive.
//   - resyncPeriod: if non-zero, will re-list this often (you will get OnUpdate
//     calls, even if nothing changed). Otherwise, re-list will be delayed as
//     long as possible (until the upstream source closes the watch or times out,
//     or you stop the controller).
//   - h is the object you want notifications sent to.
//   - clientState is the store you want to populate
func newInformer(
	lw cache.ListerWatcher,
	objType runtime.Object,
	resyncPeriod time.Duration,
	h cache.ResourceEventHandler,
	clientState cache.Store,
	transformer cache.TransformFunc,
) cache.Controller {
	// This will hold incoming changes. Note how we pass clientState in as a
	// KeyLister, that way resync operations will result in the correct set
	// of update/delete deltas.
	fifo := cache.NewDeltaFIFOWithOptions(cache.DeltaFIFOOptions{
		KnownObjects:          clientState,
		EmitDeltaTypeReplaced: true,
	})

	cfg := &cache.Config{
		Queue:            fifo,
		ListerWatcher:    lw,
		ObjectType:       objType,
		FullResyncPeriod: resyncPeriod,
		RetryOnError:     false,

		Process: func(obj interface{}, isInInitialList bool) error {
			if deltas, ok := obj.(cache.Deltas); ok {
				return processDeltas(h, clientState, transformer, deltas, isInInitialList)
			}
			return errors.New("object given as Process argument is not Deltas")
		},
	}
	return cache.New(cfg)
}
