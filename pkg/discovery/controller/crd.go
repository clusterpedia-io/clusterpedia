package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"

	internal "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
)

type CRDController struct {
	cluster string
	client  clientset.Interface

	lock      sync.Mutex
	version   string
	rebuildCh chan struct{}

	lastdone chan struct{}
	store    cache.Store
	informer cache.Controller
	handler  cache.ResourceEventHandler
	builder  func() cache.Controller
}

func NewCRDController(cluster string, config *rest.Config, version string, handler CRDEventHandlerFuncs) (*CRDController, error) {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	controller := &CRDController{
		cluster: cluster,
		client:  client,

		handler:   handler,
		store:     cache.NewStore(cache.DeletionHandlingMetaNamespaceKeyFunc),
		rebuildCh: make(chan struct{}, 1),
		lastdone:  make(chan struct{}),
	}
	close(controller.lastdone)
	if err := controller.SetVersion(version); err != nil {
		return nil, err
	}
	return controller, nil
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

	var builder func() cache.Controller
	switch version {
	case v1.SchemeGroupVersion.Version:
		builder = func() cache.Controller {
			return informer.NewInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return c.client.ApiextensionsV1().CustomResourceDefinitions().List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return c.client.ApiextensionsV1().CustomResourceDefinitions().Watch(context.TODO(), options)
					},
				},
				&v1.CustomResourceDefinition{},
				c.store,
				0,
				c.handler,
				nil)
		}
	case v1beta1.SchemeGroupVersion.Version:
		builder = func() cache.Controller {
			return informer.NewInformer(
				&cache.ListWatch{
					ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
						return c.client.ApiextensionsV1beta1().CustomResourceDefinitions().List(context.TODO(), options)
					},
					WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
						return c.client.ApiextensionsV1beta1().CustomResourceDefinitions().Watch(context.TODO(), options)
					},
				},
				&v1beta1.CustomResourceDefinition{},
				c.store,
				0,
				c.handler,
				convertV1beta1ToV1)
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

func convertV1beta1ToV1(obj interface{}) (interface{}, error) {
	if _, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		return obj, nil
	}

	internal, err := scheme.LegacyResourceScheme.ConvertToVersion(obj.(runtime.Object), internal.SchemeGroupVersion)
	if err != nil {
		return nil, err
	}
	return scheme.LegacyResourceScheme.ConvertToVersion(internal, v1.SchemeGroupVersion)
}

type CRDEventHandlerFuncs struct {
	Name string

	AddFunc func(obj *v1.CustomResourceDefinition)

	UpdateFunc          func(older *v1.CustomResourceDefinition, newer *v1.CustomResourceDefinition)
	UpdateFuncOnlyNewer func(newer *v1.CustomResourceDefinition)

	// cache.DeletedFinalStateUnknown will be ignored
	DeleteFunc func(obj *v1.CustomResourceDefinition)
}

func (handler CRDEventHandlerFuncs) OnAdd(obj interface{}, isInInitialList bool) {
	if handler.AddFunc == nil {
		return
	}
	crd, ok := obj.(*v1.CustomResourceDefinition)
	if !ok {
		klog.ErrorS(errors.New("CRDEventHandler OnAdd: newer obj is not *v1.CustomResourceDefinition"), "cluster", handler.Name)
		return
	}
	handler.AddFunc(crd)
}

func (handler CRDEventHandlerFuncs) OnUpdate(older, newer interface{}) {
	if handler.UpdateFunc == nil && handler.UpdateFuncOnlyNewer == nil {
		return
	}

	newerCRD, ok := newer.(*v1.CustomResourceDefinition)
	if !ok {
		klog.ErrorS(errors.New("CRDEventHandler OnUpdate: newer obj is not *v1.CustomResourceDefinition"), "cluster", handler.Name)
		return
	}

	if handler.UpdateFunc != nil {
		olderCRD, ok := older.(*v1.CustomResourceDefinition)
		if !ok {
			klog.ErrorS(errors.New("CRDEventHandler OnUpdate: older obj is not *v1.CustomResourceDefinition"), "cluster", handler.Name)
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

	crd, ok := obj.(*v1.CustomResourceDefinition)
	if !ok {
		klog.ErrorS(errors.New("CRDEventHandler OnDelete: obj is not *v1.CustomResourceDefinition"), "cluster", handler.Name)
		return
	}
	handler.DeleteFunc(crd)
}
