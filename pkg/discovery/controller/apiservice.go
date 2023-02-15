package controller

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	v1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	clientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	informers "k8s.io/kube-aggregator/pkg/client/informers/externalversions/apiregistration/v1"
	listers "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1"
)

type APIServiceController struct {
	cluster string
	client  clientset.Interface

	reconciled  bool
	reconcileCh chan struct{}
	reconciler  func(apiservices []*v1.APIService)

	lock     sync.Mutex
	lastdone chan struct{}
	informer cache.SharedIndexInformer
}

func NewAPIServiceController(cluster string, config *rest.Config, reconciler func(apiservices []*v1.APIService)) (*APIServiceController, error) {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	controller := &APIServiceController{
		cluster: cluster,
		client:  client,

		reconciler:  reconciler,
		reconcileCh: make(chan struct{}, 1),

		lastdone: make(chan struct{}),
	}
	close(controller.lastdone)
	return controller, nil
}

func (c *APIServiceController) Start(stopCh <-chan struct{}) <-chan struct{} {
	informer := c.genInformer(stopCh)
	lister := listers.NewAPIServiceLister(informer.GetIndexer())

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
		c.informer = informer
		c.lock.Unlock()

		go informer.Run(stopCh)
		cache.WaitForCacheSync(stopCh, informer.HasSynced)

		for {
			select {
			case <-c.reconcileCh:
			case <-stopCh:
				return
			}

			select {
			case <-stopCh:
				return
			default:
			}

			c.reconcile(lister)
			c.reconciled = true
		}
	}()

	return done
}

func (c *APIServiceController) genInformer(stopCh <-chan struct{}) cache.SharedIndexInformer {
	informer := informers.NewAPIServiceInformer(c.client, 0, nil)
	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(_ interface{}) { c.trigger(stopCh) },
		UpdateFunc: func(old, new interface{}) {
			oldObj, oldOk := old.(*v1.APIService)
			newObj, newOk := new.(*v1.APIService)
			if !oldOk || !newOk {
				return
			}
			if equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
				if oldObj.Spec.Service == nil || equality.Semantic.DeepEqual(oldObj.Status, newObj.Status) {
					return
				}
			}
			c.trigger(stopCh)
		},
		DeleteFunc: func(_ interface{}) { c.trigger(stopCh) },
	}); err != nil {
		klog.ErrorS(err, "error when adding event handler to informer")
	}

	return informer
}

func (c *APIServiceController) trigger(stopCh <-chan struct{}) {
	select {
	case <-stopCh:
		return
	default:
	}

	select {
	case c.reconcileCh <- struct{}{}:
	default:
	}
}

func (c *APIServiceController) reconcile(lister listers.APIServiceLister) {
	apiservices, err := lister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "reconcile apiservices failed", "cluster", c.cluster)
		return
	}
	c.reconciler(apiservices)
}

func (c *APIServiceController) HasSynced() bool {
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
	return c.informer.HasSynced() && c.reconciled
}
