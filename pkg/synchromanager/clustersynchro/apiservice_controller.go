package clustersynchro

import (
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	apiregistrationv1api "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiregistrationv1helper "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1/helper"
	aggregatorclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	apiregistrationinformers "k8s.io/kube-aggregator/pkg/client/informers/externalversions/apiregistration/v1"
	apiregistrationlisters "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1"

	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/discovery"
)

type APIServiceController struct {
	cluster string

	reconciled     bool
	reconcileCh    chan struct{}
	discoveryCache discovery.GroupVersionCache

	informer cache.SharedIndexInformer
	lister   apiregistrationlisters.APIServiceLister
}

func NewAPIServiceController(cluster string, config *rest.Config, discoveryCache discovery.GroupVersionCache) (*APIServiceController, error) {
	client, err := aggregatorclientset.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	informer := apiregistrationinformers.NewAPIServiceInformer(client, 0, nil)
	controller := &APIServiceController{
		cluster:        cluster,
		discoveryCache: discoveryCache,
		reconcileCh:    make(chan struct{}, 1),
		informer:       informer,
		lister:         apiregistrationlisters.NewAPIServiceLister(informer.GetIndexer()),
	}

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(_ interface{}) { controller.triggerReconcile() },
		UpdateFunc: func(old, new interface{}) {
			oldObj, oldOk := old.(*apiregistrationv1api.APIService)
			newObj, newOk := new.(*apiregistrationv1api.APIService)
			if !oldOk || !newOk {
				return
			}
			if equality.Semantic.DeepEqual(oldObj.Spec, newObj.Spec) {
				if oldObj.Spec.Service == nil || equality.Semantic.DeepEqual(oldObj.Status, newObj.Status) {
					return
				}
			}
			controller.triggerReconcile()
		},
		DeleteFunc: func(_ interface{}) { controller.triggerReconcile() },
	})
	return controller, nil
}

func (c *APIServiceController) triggerReconcile() {
	select {
	case c.reconcileCh <- struct{}{}:
	default:
	}
}

func (c *APIServiceController) Run(stopCh <-chan struct{}) {
	go c.informer.Run(stopCh)
	cache.WaitForCacheSync(stopCh, c.informer.HasSynced)

	select {
	case <-c.reconcileCh:
	default:
	}
	c.reconcile()
	c.reconciled = true

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
		c.reconcile()
	}
}

func (c *APIServiceController) HasSynced() bool {
	return c.informer.HasSynced() && c.reconciled
}

func (c *APIServiceController) reconcile() {
	apiservices, err := c.lister.List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "reconcile apiservices failed", "cluster", c.cluster)
		return
	}

	groupVersions := make(map[string][]string)
	aggregatorGroups := sets.NewString()

	apiServicesByGroup := apiregistrationv1helper.SortedByGroupAndVersion(apiservices)
	for _, groupServices := range apiServicesByGroup {
		apiServicesByGroup := apiregistrationv1helper.SortedByGroupAndVersion(groupServices)[0]
		group := apiServicesByGroup[0].Spec.Group

		var versions []string
		var isLocal, isAggregator bool
		for _, apiService := range apiServicesByGroup {
			if apiService.Spec.Service != nil {
				isAggregator = true
				if !apiregistrationv1helper.IsAPIServiceConditionTrue(apiService, apiregistrationv1api.Available) {
					// ignore unavailable aggregator version
					continue
				}
			} else {
				isLocal = true
			}
			versions = append(versions, apiService.Spec.Version)
		}

		if isLocal && isAggregator {
			// the versions of the group contain Local and Aggregator, ignoring such group
			continue
		}

		if len(versions) == 0 {
			continue
		}

		groupVersions[group] = versions
		if isAggregator {
			aggregatorGroups.Insert(group)
		}
	}

	// The kube-aggregator does not register `apiregistration.k8s.io` as an APIService resource, we need to add `apiregistration.k8s.io/v1` to the **groupVersions**
	groupVersions[apiregistrationv1api.SchemeGroupVersion.Group] = []string{apiregistrationv1api.SchemeGroupVersion.Version}

	// full update, ensuring version order
	c.discoveryCache.SetGroupVersions(groupVersions, aggregatorGroups)
}
