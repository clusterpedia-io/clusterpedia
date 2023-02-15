package controller

import (
	"fmt"
	"sync"

	"go.uber.org/atomic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/dynamic/dynamiclister"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	policyv1alpha1 "github.com/clusterpedia-io/api/policy/v1alpha1"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
)

type DependentResourceManager struct {
	lock sync.RWMutex

	policyQueue          workqueue.RateLimitingInterface
	lifecycleQueue       workqueue.RateLimitingInterface
	listerWatcherFactory informer.DynamicListerWatcherFactory

	listers           map[schema.GroupVersionResource]dynamiclister.Lister
	controllers       map[schema.GroupVersionResource]cache.Controller
	controllerStopChs map[schema.GroupVersionResource]chan struct{}

	sourceToPolicy     map[schema.GroupVersionResource]string
	policyToSource     map[string]schema.GroupVersionResource
	policyCouldEnqueue map[string]*atomic.Bool
	policyToGVRs       map[string]map[schema.GroupVersionResource]struct{}
	gvrToPolicies      map[schema.GroupVersionResource]sets.Set[string]

	referencesToLifecycle map[policyv1alpha1.DependentResource]sets.Set[string]
	lifecycleToReferences map[string]map[policyv1alpha1.DependentResource]struct{}
}

func NewDependentResourceManager(
	policyQueue workqueue.RateLimitingInterface,
	lifecycleQueue workqueue.RateLimitingInterface,
	listerWatcherFactory informer.DynamicListerWatcherFactory,
) *DependentResourceManager {
	return &DependentResourceManager{
		policyQueue:          policyQueue,
		lifecycleQueue:       lifecycleQueue,
		listerWatcherFactory: listerWatcherFactory,

		listers:           make(map[schema.GroupVersionResource]dynamiclister.Lister),
		controllers:       make(map[schema.GroupVersionResource]cache.Controller),
		controllerStopChs: make(map[schema.GroupVersionResource]chan struct{}),

		sourceToPolicy:     make(map[schema.GroupVersionResource]string),
		policyToSource:     make(map[string]schema.GroupVersionResource),
		policyCouldEnqueue: make(map[string]*atomic.Bool),
		policyToGVRs:       make(map[string]map[schema.GroupVersionResource]struct{}),
		gvrToPolicies:      make(map[schema.GroupVersionResource]sets.Set[string]),

		referencesToLifecycle: make(map[policyv1alpha1.DependentResource]sets.Set[string]),
		lifecycleToReferences: make(map[string]map[policyv1alpha1.DependentResource]struct{}),
	}
}

func (manager *DependentResourceManager) getLister(gvr schema.GroupVersionResource) dynamiclister.Lister {
	manager.lock.RLock()
	defer manager.lock.RUnlock()
	return manager.listers[gvr]
}

func (manager *DependentResourceManager) Get(resource policyv1alpha1.DependentResource) (*unstructured.Unstructured, error) {
	gvr := resource.GroupVersionResource()
	lister := manager.getLister(gvr)
	if lister == nil {
		return nil, fmt.Errorf("resource<%s> lister is not found", gvr)
	}

	if resource.Namespace == "" {
		return lister.Get(resource.Name)
	}
	return lister.Namespace(resource.Namespace).Get(resource.Name)
}

func (manager *DependentResourceManager) List(gvr schema.GroupVersionResource) (ret []*unstructured.Unstructured, err error) {
	lister := manager.getLister(gvr)
	if lister == nil {
		return nil, fmt.Errorf("resource<%s> lister is not found", gvr)
	}

	return lister.List(labels.Everything())
}

func (manager *DependentResourceManager) genDependentResourceHandler(gvr schema.GroupVersionResource) func(interface{}) {
	return func(obj interface{}) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			klog.ErrorS(err, "handle dependent resource failed")
			return
		}

		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			klog.ErrorS(err, "handle dependent resource failed", "key", key)
			return
		}

		manager.lock.RLock()
		defer manager.lock.RUnlock()

		if policyName, ok := manager.sourceToPolicy[gvr]; ok {
			if could := manager.policyCouldEnqueue[policyName]; ok && could.Load() {
				klog.V(3).InfoS("add source to policy queue", "source gvr", gvr, "source", key, "policy", policyName)
				manager.policyQueue.Add(policyName)
			}
		}

		dependence := policyv1alpha1.DependentResource{Group: gvr.Group, Version: gvr.Version, Resource: gvr.Resource,
			Namespace: namespace, Name: name}
		lifecycles := manager.referencesToLifecycle[dependence]
		if len(lifecycles) == 0 {
			return
		}

		klog.V(3).InfoS("add depent resource to lifecycle queue", "gvr", gvr, "resource", key, "lifecycles", lifecycles)
		for lifecycle := range lifecycles {
			manager.lifecycleQueue.Add(lifecycle)
		}
	}
}

func (manager *DependentResourceManager) ensureResourceController(gvr schema.GroupVersionResource) {
	if _, ok := manager.controllers[gvr]; ok {
		return
	}
	klog.InfoS("create and start dependent resource informer", "gvr", gvr)

	handler := manager.genDependentResourceHandler(gvr)
	lw := manager.listerWatcherFactory.ForResource(metav1.NamespaceAll, gvr)
	indexer, controller := cache.NewIndexerInformer(lw, &unstructured.Unstructured{}, 0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    handler,
			UpdateFunc: func(_, obj interface{}) { handler(obj) },
			DeleteFunc: handler,
		},
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	stopCh := make(chan struct{})
	go controller.Run(stopCh)

	manager.listers[gvr] = dynamiclister.New(indexer, gvr)
	manager.controllers[gvr] = controller
	manager.controllerStopChs[gvr] = stopCh
}

func (manager *DependentResourceManager) unbindPolicyDependentGVR(name string, gvr schema.GroupVersionResource) {
	policies := manager.gvrToPolicies[gvr]
	policies.Delete(name)

	if len(policies) == 0 {
		klog.InfoS("stop and remove depent resource informer", "gvr", gvr)
		delete(manager.gvrToPolicies, gvr)

		if stopCh, ok := manager.controllerStopChs[gvr]; ok {
			close(stopCh)
			delete(manager.controllerStopChs, gvr)
		}
		delete(manager.listers, gvr)
		delete(manager.controllers, gvr)
	}
}

func (manager *DependentResourceManager) setPolicyDependentGVRs(name string, gvrs map[schema.GroupVersionResource]struct{}) {
	currentGVRs := manager.policyToGVRs[name]
	if gvrs == nil {
		for gvr := range currentGVRs {
			manager.unbindPolicyDependentGVR(name, gvr)
		}
		delete(manager.policyToGVRs, name)
		return
	}

	for gvr := range currentGVRs {
		if _, ok := gvrs[gvr]; !ok {
			manager.unbindPolicyDependentGVR(name, gvr)
		}
	}

	if currentGVRs == nil {
		currentGVRs = make(map[schema.GroupVersionResource]struct{}, 0)
	}
	for gvr := range gvrs {
		if _, ok := currentGVRs[gvr]; !ok {
			manager.ensureResourceController(gvr)
			if policies, ok := manager.gvrToPolicies[gvr]; ok {
				policies.Insert(name)
			} else {
				manager.gvrToPolicies[gvr] = sets.New(name)
			}
		}
	}
	manager.policyToGVRs[name] = gvrs
}

func (manager *DependentResourceManager) SetPolicyDependentGVRs(name string, source schema.GroupVersionResource, references map[schema.GroupVersionResource]struct{}) error {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	if policy, ok := manager.sourceToPolicy[source]; ok && policy != name {
		return fmt.Errorf("source<%s> is already bound to %s", source, policy)
	}

	manager.policyToSource[name] = source
	manager.sourceToPolicy[source] = name
	manager.policyCouldEnqueue[name] = atomic.NewBool(false)

	references[source] = struct{}{}
	manager.setPolicyDependentGVRs(name, references)
	return nil
}

func (manager *DependentResourceManager) RemovePolicy(name string) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	manager.setPolicyDependentGVRs(name, nil)
	if enqueue, ok := manager.policyCouldEnqueue[name]; ok {
		enqueue.Store(false)
		delete(manager.policyCouldEnqueue, name)
	}

	if source, ok := manager.policyToSource[name]; ok {
		delete(manager.policyToSource, name)
		delete(manager.sourceToPolicy, source)
	}
}

func (manager *DependentResourceManager) HasSyncedPolicyDependentResources(name string) (bool, error) {
	manager.lock.RLock()
	defer manager.lock.RUnlock()

	gvrs, ok := manager.policyToGVRs[name]
	if !ok {
		return false, nil
	}

	for gvr := range gvrs {
		controller, ok := manager.controllers[gvr]
		if !ok {
			return false, fmt.Errorf("resource<%s> controller not found", gvr)
		}
		if !controller.HasSynced() {
			return false, nil
		}
	}

	if could, ok := manager.policyCouldEnqueue[name]; ok {
		could.Store(true)
	}
	return true, nil
}

func (manager *DependentResourceManager) setLifecycleDependentResources(name string, references map[policyv1alpha1.DependentResource]struct{}) {
	currentReferences := manager.lifecycleToReferences[name]
	if references == nil {
		for ref := range currentReferences {
			delete(manager.referencesToLifecycle[ref], name)
			if len(manager.referencesToLifecycle[ref]) == 0 {
				delete(manager.referencesToLifecycle, ref)
			}
		}
		delete(manager.lifecycleToReferences, name)
		return
	}

	// delete references that are not in `references`.
	for ref := range currentReferences {
		if _, ok := references[ref]; !ok {
			delete(manager.referencesToLifecycle[ref], name)
			if len(manager.referencesToLifecycle[ref]) == 0 {
				delete(manager.referencesToLifecycle, ref)
			}
		}
	}

	if currentReferences == nil {
		currentReferences = make(map[policyv1alpha1.DependentResource]struct{}, 0)
	}
	// add references that are not in `currentReferences`
	for ref := range references {
		if _, ok := currentReferences[ref]; !ok {
			if lifecycles := manager.referencesToLifecycle[ref]; lifecycles == nil {
				manager.referencesToLifecycle[ref] = sets.New(name)
			} else {
				lifecycles.Insert(name)
			}
		}
	}
	manager.lifecycleToReferences[name] = references
}

func (manager *DependentResourceManager) SetLifecycleDependentResources(name string, references map[policyv1alpha1.DependentResource]struct{}) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	manager.setLifecycleDependentResources(name, references)
}

func (manager *DependentResourceManager) RemoveLifecycle(name string) {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	manager.setLifecycleDependentResources(name, nil)
}
