package controller

import (
	"fmt"
	"sync"

	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	v1informer "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	v1beta1informer "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
)

type CRDController struct {
	cluster string
	client  clientset.Interface

	lock      sync.Mutex
	version   string
	rebuildCh chan struct{}

	lastdone chan struct{}
	informer cache.SharedIndexInformer
	handlers []cache.ResourceEventHandler
	builder  func() cache.SharedIndexInformer
}

func NewCRDController(cluster string, config *rest.Config, version string) (*CRDController, error) {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	controller := &CRDController{
		cluster: cluster,
		client:  client,

		rebuildCh: make(chan struct{}, 1),
		lastdone:  make(chan struct{}),
	}
	close(controller.lastdone)
	if err := controller.SetVersion(version); err != nil {
		return nil, err
	}
	return controller, nil
}

func (c *CRDController) AddEventHandler(handler cache.ResourceEventHandler) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.handlers = append(c.handlers, handler)
	if c.informer != nil {
		c.informer.AddEventHandler(handler)
	}
}

func (c *CRDController) Start(stopCh <-chan struct{}) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)

	retry:
		for {
			c.lock.Lock()
			lastdone := c.lastdone

			select {
			case <-lastdone:
				select {
				case <-stopCh:
					c.lock.Unlock()
					return
				default:
					break retry
				}
			case <-stopCh:
				c.lock.Unlock()
				return
			default:
				c.lock.Unlock()
			}

			select {
			case <-lastdone:
			case <-stopCh:
				return
			}
		}

		c.lastdone = done
		c.informer = nil
		c.lock.Unlock()

		for {
			stop := make(chan struct{})
			c.lock.Lock()

			select {
			case <-c.rebuildCh:
			default:
			}

			go func() {
				defer close(stop)

				select {
				case <-stopCh:
				case <-c.rebuildCh:
				}

				c.lock.Lock()
				c.informer = nil
				c.lock.Unlock()
			}()

			informer := c.builder()
			for _, handler := range c.handlers {
				informer.AddEventHandler(handler)
			}
			c.informer = informer

			c.lock.Unlock()

			informer.Run(stop)
			select {
			case <-stopCh:
				return
			default:
			}
		}
	}()

	return done
}

func (c *CRDController) SetVersion(version string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.version == version {
		return nil
	}

	var builder func() cache.SharedIndexInformer
	switch version {
	case v1.SchemeGroupVersion.Version:
		builder = func() cache.SharedIndexInformer {
			return v1informer.NewCustomResourceDefinitionInformer(c.client, 0, nil)
		}
	case v1beta1.SchemeGroupVersion.Version:
		builder = func() cache.SharedIndexInformer {
			return v1beta1informer.NewCustomResourceDefinitionInformer(c.client, 0, nil)
		}
	default:
		return fmt.Errorf("CRD Version %s is not supported", version)
	}

	c.version = version
	c.builder = builder
	c.informer = nil

	select {
	case c.rebuildCh <- struct{}{}:
	default:
	}
	return nil
}

func (c *CRDController) HasSynced() bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	select {
	case <-c.lastdone:
		return false
	default:
	}

	if c.informer == nil {
		return false
	}
	return c.informer.HasSynced()
}

type CRDEventHandlerFuncs struct {
	AddFunc func(obj *v1.CustomResourceDefinition)

	UpdateFunc          func(older *v1.CustomResourceDefinition, newer *v1.CustomResourceDefinition)
	UpdateFuncOnlyNewer func(newer *v1.CustomResourceDefinition)

	// cache.DeletedFinalStateUnknown will be ignored
	DeleteFunc func(obj *v1.CustomResourceDefinition)
}

func (handler CRDEventHandlerFuncs) OnAdd(obj interface{}) {
	if handler.AddFunc == nil {
		return
	}
	runtimeobj, err := scheme.LegacyResourceScheme.ConvertToVersion(obj.(runtime.Object), v1.SchemeGroupVersion)
	if err != nil {
		return
	}
	crd, ok := runtimeobj.(*v1.CustomResourceDefinition)
	if !ok {
		return
	}
	handler.AddFunc(crd)
}

func (handler CRDEventHandlerFuncs) OnUpdate(older, newer interface{}) {
	if handler.UpdateFunc == nil && handler.UpdateFuncOnlyNewer == nil {
		return
	}

	newerObj, err := scheme.LegacyResourceScheme.ConvertToVersion(newer.(runtime.Object), v1.SchemeGroupVersion)
	if err != nil {
		return
	}
	newerCRD, ok := newerObj.(*v1.CustomResourceDefinition)
	if !ok {
		return
	}

	if handler.UpdateFunc != nil {
		olderObj, err := scheme.LegacyResourceScheme.ConvertToVersion(older.(runtime.Object), v1.SchemeGroupVersion)
		if err != nil {
			return
		}
		olderCRD, ok := olderObj.(*v1.CustomResourceDefinition)
		if !ok {
			return
		}

		handler.UpdateFunc(olderCRD, newerCRD)
	}

	if handler.UpdateFuncOnlyNewer != nil {
		handler.UpdateFuncOnlyNewer(newerCRD)
	}
}

func (handler CRDEventHandlerFuncs) OnDelete(obj interface{}) {
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if tombstone.Obj == nil {
			// klog.ErrorS(errors.New("Couldn't get object from tombstone"), "cluster", c.cluster, "object", obj)
			return
		}
		obj = tombstone.Obj
	}

	runtimeobj, ok := obj.(runtime.Object)
	if !ok {
		return
	}
	runtimeobj, err := scheme.LegacyResourceScheme.ConvertToVersion(runtimeobj, v1.SchemeGroupVersion)
	if err != nil {
		// klog.ErrorS(errors.New("object that is not expected"), "cluster", c.cluster, "object", obj)
		return
	}
	crd, ok := runtimeobj.(*v1.CustomResourceDefinition)
	if !ok {
		return
	}
	handler.DeleteFunc(crd)
}
