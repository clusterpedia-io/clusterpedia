package discovery

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/discovery/controller"
)

type DynamicDiscoveryInterface interface {
	ServerVersionInterface
	ResourcesInterface

	Prepare(c interface{})
	Start(stopCh <-chan struct{})

	WatchServerVersion(watch bool)
	WatchAggregatorResourceTypes(watch bool)
}

type DynamicDiscoveryManager struct {
	name      string
	discovery discovery.DiscoveryInterface
	version   atomic.Value // version.Info

	// TODO(Iceber): Split into a new struct, that could maybe be named discoveryCache
	cacheLock     sync.RWMutex
	groupVersions map[string][]string

	customResourceGroups sets.Set[string]
	aggregatorGroups     sets.Set[string]

	// only include kube resources and aggregator resources,
	// not have custom resources
	resourceVersions map[schema.GroupResource][]string
	apiResources     map[schema.GroupResource]metav1.APIResource

	pluralToSingular map[schema.GroupResource]schema.GroupResource
	singularToPlural map[schema.GroupResource]schema.GroupResource

	dirty                   atomic.Bool
	enabledMutationHandler  atomic.Bool
	resourceMutationHandler func()
	afterStartFunc          func(stopCh <-chan struct{})

	runLock                       sync.Mutex
	lastdone                      chan struct{}
	stopCh                        <-chan struct{}
	versionWatchStopCh            chan struct{}
	aggregatorResourceWatchStopCh chan struct{}

	crdController        *controller.CRDController
	apiServiceController *controller.APIServiceController

	updateCRDVersionError error
}

var _ DynamicDiscoveryInterface = &DynamicDiscoveryManager{}

func NewDynamicDiscoveryManager(name string, config *rest.Config) (*DynamicDiscoveryManager, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create a discovery client: %w", err)
	}

	version, err := discoveryClient.ServerVersion()
	if err != nil {
		return nil, err
	}

	apiGroupList, err := discoveryClient.ServerGroups()
	if err != nil {
		return nil, err
	}

	manager := &DynamicDiscoveryManager{
		name:      name,
		discovery: discoveryClient,

		groupVersions:        make(map[string][]string),
		customResourceGroups: sets.Set[string]{},
		aggregatorGroups:     sets.Set[string]{},

		resourceVersions: make(map[schema.GroupResource][]string),
		apiResources:     make(map[schema.GroupResource]metav1.APIResource),
		pluralToSingular: make(map[schema.GroupResource]schema.GroupResource),
		singularToPlural: make(map[schema.GroupResource]schema.GroupResource),

		lastdone: make(chan struct{}),
	}
	close(manager.lastdone)
	manager.version.Store(*version)
	manager.afterStartFunc = func(_ <-chan struct{}) {}
	manager.resourceMutationHandler = manager.checkAndUpdateCRDVersion

	for _, apiGroup := range apiGroupList.Groups {
		for _, version := range apiGroup.Versions {
			manager.groupVersions[apiGroup.Name] = append(manager.groupVersions[apiGroup.Name], version.Version)
		}
	}
	_ = manager.refetchAllGroups()

	_, crdVersions := manager.GetAPIResourceAndVersions(schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"})
	if len(crdVersions) == 0 {
		return nil, fmt.Errorf("not match crd version")
	}

	manager.crdController, err = controller.NewCRDController(name, config, crdVersions[0], controller.CRDEventHandlerFuncs{
		Name:                manager.name,
		AddFunc:             manager.updateCustomResource,
		UpdateFuncOnlyNewer: manager.updateCustomResource,
		DeleteFunc:          manager.removeCustomResource,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create crd controller: %w", err)
	}

	manager.apiServiceController, err = controller.NewAPIServiceController(name, config, manager.reconcileAPIServices)
	if err != nil {
		return nil, fmt.Errorf("failed to create apiservice controller: %w", err)
	}
	return manager, nil
}

type PrepareConfig struct {
	ResourceMutationHandler func()
	AfterStartFunc          func(stopCh <-chan struct{})
}

func (c *DynamicDiscoveryManager) Prepare(cfg interface{}) {
	pc, ok := cfg.(PrepareConfig)
	if !ok {
		return
	}

	if pc.ResourceMutationHandler != nil {
		f := pc.ResourceMutationHandler
		c.resourceMutationHandler = func() {
			c.checkAndUpdateCRDVersion()
			f()
		}
	}

	if pc.AfterStartFunc != nil {
		c.afterStartFunc = pc.AfterStartFunc
	}
}

func (c *DynamicDiscoveryManager) checkAndUpdateCRDVersion() {
	_, crdVersions := c.GetAPIResourceAndVersions(schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"})
	if len(crdVersions) == 0 {
		c.updateCRDVersionError = fmt.Errorf("not match crd version")
		return
	}
	c.updateCRDVersionError = c.crdController.SetVersion(crdVersions[0])
}

func (c *DynamicDiscoveryManager) Start(stopCh <-chan struct{}) {
retry:
	for {
		c.runLock.Lock()
		lastdone := c.lastdone

		select {
		case <-lastdone:
			select {
			case <-stopCh:
				c.runLock.Unlock()
				return
			default:
				break retry
			}
		case <-stopCh:
			c.runLock.Unlock()
			return
		default:
			c.runLock.Unlock()
		}

		klog.V(3).InfoS("[dyanmic discovery manager] waiting for the last run to be done", "cluster", c.name)
		select {
		case <-lastdone:
		case <-stopCh:
			klog.V(3).InfoS("[dyanmic discovery manager] stop waiting for the last run to be done, because stopCh is closed", "cluster", c.name)
			return
		}
	}

	klog.InfoS("start dynamic discovery manager", "cluster", c.name)
	done := make(chan struct{})
	defer func() {
		klog.InfoS("dynamic discovery manager is stopped", "cluster", c.name)
		close(done)
	}()

	c.lastdone = done
	c.stopCh = stopCh

	c.startAggregatorResourceWatcher()
	c.startServerVersionWatcher()
	c.runLock.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		<-c.crdController.Start(stopCh)
		wg.Done()
	}()
	klog.InfoS("Waiting for caches to sync for DynamicDiscoveryManager's CRD Controller", "cluster", c.name)
	// First initialize the information for the custom resources to dynamic discovery manager
	if !cache.WaitForCacheSync(stopCh, c.crdController.HasSynced) {
		wg.Wait()
		return
	}
	klog.InfoS("Caches are synced for DynamicDiscoveryManager's CRD Controller", "cluster", c.name)

	wg.Add(1)
	go func() {
		<-c.apiServiceController.Start(stopCh)
		wg.Done()
	}()
	klog.InfoS("Waiting for caches to sync for DynamicDiscoveryManager's APIService Controller", "cluster", c.name)
	// First initialize the information for the custom resources to dynamic discovery manager
	if !cache.WaitForCacheSync(stopCh, c.apiServiceController.HasSynced) {
		wg.Wait()
		return
	}
	klog.InfoS("Caches are synced for DynamicDiscoveryManager's APIService Controller", "cluster", c.name)

	c.enableMutationHandler()
	defer c.disableMutationHandler()

	if c.dirty.Load() {
		c.resourceMutationHandler()
		c.dirty.Store(false)
	}
	c.afterStartFunc(stopCh)

	wg.Wait()
}

func (c *DynamicDiscoveryManager) startedLocked() bool {
	if c.stopCh == nil {
		return false
	}

	select {
	case <-c.stopCh:
		return false
	default:
		return true
	}
}

func (c *DynamicDiscoveryManager) WatchServerVersion(watch bool) {
	c.runLock.Lock()
	defer c.runLock.Unlock()
	if !watch {
		c.stopServerVersionWatcher()
		return
	}

	if c.versionWatchStopCh != nil {
		return
	}

	c.versionWatchStopCh = make(chan struct{})
	c.startServerVersionWatcher()
}

func (c *DynamicDiscoveryManager) startServerVersionWatcher() {
	if c.versionWatchStopCh == nil || !c.startedLocked() {
		return
	}

	klog.InfoS("start server version watcher", "cluster", c.name)
	stopCh := make(chan struct{})
	go func(stopCh1, stopCh2 <-chan struct{}) {
		select {
		case <-stopCh1:
		case <-stopCh2:
		}
		close(stopCh)
		klog.InfoS("stop server version watcher", "cluster", c.name)
	}(c.versionWatchStopCh, c.stopCh)

	go wait.Until(func() {
		if _, err := c.GetAndFetchServerVersion(); err != nil {
			utilruntime.HandleError(err)
		}
	}, 5*time.Minute, stopCh)
}

func (c *DynamicDiscoveryManager) stopServerVersionWatcher() {
	if c.versionWatchStopCh == nil {
		return
	}
	close(c.versionWatchStopCh)
	c.versionWatchStopCh = nil
}

func (c *DynamicDiscoveryManager) WatchAggregatorResourceTypes(watch bool) {
	c.runLock.Lock()
	defer c.runLock.Unlock()
	if !watch {
		c.stopAggregatorResourceWatcher()
		return
	}

	if c.aggregatorResourceWatchStopCh != nil {
		return
	}
	c.aggregatorResourceWatchStopCh = make(chan struct{})
	c.startAggregatorResourceWatcher()
}

func (c *DynamicDiscoveryManager) startAggregatorResourceWatcher() {
	if c.aggregatorResourceWatchStopCh == nil || !c.startedLocked() {
		return
	}

	klog.InfoS("start aggregstor resource watcher", "cluster", c.name)
	stopCh := make(chan struct{})
	go func(stopCh1, stopCh2 <-chan struct{}) {
		select {
		case <-stopCh1:
		case <-stopCh2:
		}

		close(stopCh)
		klog.InfoS("stop aggregstor resource watcher", "cluster", c.name)
	}(c.aggregatorResourceWatchStopCh, c.stopCh)

	go wait.Until(func() { _ = c.refetchAggregatorGroups() }, 5*time.Minute, stopCh)
}

func (c *DynamicDiscoveryManager) stopAggregatorResourceWatcher() {
	if c.aggregatorResourceWatchStopCh == nil {
		return
	}

	close(c.aggregatorResourceWatchStopCh)
	c.aggregatorResourceWatchStopCh = nil
}
