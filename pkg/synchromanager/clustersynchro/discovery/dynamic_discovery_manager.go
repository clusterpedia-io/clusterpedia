package discovery

import (
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
)

type DynamicDiscoveryManager struct {
	name      string
	discovery discovery.DiscoveryInterface
	version   atomic.Value // version.Info

	watchLock                      sync.Mutex
	stopCh                         <-chan struct{}
	versionWatchStopCh             chan struct{}
	aggregatorResourceWatchStopCh  chan struct{}
	versionWatchByResurceTypeWatch bool

	lock          sync.RWMutex
	groupVersions map[string][]string

	customResourceGroups sets.String
	aggregatorGroups     sets.String

	// only include kube resources and aggregator resources,
	// not have custom resources
	resourceVersions map[schema.GroupResource][]string
	apiResources     map[schema.GroupResource]metav1.APIResource

	pluralToSingular map[schema.GroupResource]schema.GroupResource
	singularToPlural map[schema.GroupResource]schema.GroupResource

	resourceMutationHandler func()
}

var _ CustomResourceCache = &DynamicDiscoveryManager{}
var _ GroupVersionCache = &DynamicDiscoveryManager{}

func NewDynamicDiscoveryManager(name string, discovery discovery.DiscoveryInterface) (*DynamicDiscoveryManager, error) {
	version, err := discovery.ServerVersion()
	if err != nil {
		return nil, err
	}

	apiGroupList, err := discovery.ServerGroups()
	if err != nil {
		return nil, err
	}

	manager := &DynamicDiscoveryManager{
		name:      name,
		discovery: discovery,

		groupVersions:        make(map[string][]string),
		customResourceGroups: sets.NewString(),
		aggregatorGroups:     sets.NewString(),

		resourceVersions: make(map[schema.GroupResource][]string),
		apiResources:     make(map[schema.GroupResource]metav1.APIResource),
		pluralToSingular: make(map[schema.GroupResource]schema.GroupResource),
		singularToPlural: make(map[schema.GroupResource]schema.GroupResource),

		resourceMutationHandler: func() {},
	}
	manager.version.Store(*version)

	for _, apiGroup := range apiGroupList.Groups {
		for _, version := range apiGroup.Versions {
			manager.groupVersions[apiGroup.Name] = append(manager.groupVersions[apiGroup.Name], version.Version)
		}
	}
	_ = manager.refetchAllGroups()
	return manager, nil
}

func (c *DynamicDiscoveryManager) Run(stopCh <-chan struct{}) {
	// Simplifying the logic for handling multiple calls?
retry:
	c.watchLock.Lock()
	if lastStopCh := c.stopCh; lastStopCh != nil {
		select {
		case <-lastStopCh:
			goto run
		default:
		}
		c.watchLock.Unlock()

		select {
		case <-lastStopCh:
			goto retry
		case <-stopCh:
			return
		}
	}

run:
	c.stopCh = nil
	select {
	case <-stopCh:
		c.watchLock.Unlock()
		return
	default:
	}

	c.stopCh = stopCh
	c.startAggregatorResourceWatcher()
	c.startServerVersionWatcher()
	c.watchLock.Unlock()

	<-stopCh
}

func (c *DynamicDiscoveryManager) SetWatchServerVersion() {
	c.watchLock.Lock()
	defer c.watchLock.Unlock()

	c.versionWatchByResurceTypeWatch = false
	if c.versionWatchStopCh != nil {
		return
	}

	c.versionWatchStopCh = make(chan struct{})
	c.startServerVersionWatcher()
}

func (c *DynamicDiscoveryManager) StopWatchServerVersion() {
	c.watchLock.Lock()
	defer c.watchLock.Unlock()

	if c.versionWatchStopCh == nil {
		return
	}
	if c.versionWatchByResurceTypeWatch {
		// need stop by StopWatchResourceType
		return
	}

	close(c.versionWatchStopCh)
	c.versionWatchStopCh = nil
}

func (c *DynamicDiscoveryManager) SetWatchResourceType() {
	c.watchLock.Lock()
	defer c.watchLock.Unlock()
	if c.aggregatorResourceWatchStopCh != nil {
		return
	}
	c.aggregatorResourceWatchStopCh = make(chan struct{})
	c.startAggregatorResourceWatcher()

	if c.versionWatchStopCh != nil {
		return
	}
	c.versionWatchByResurceTypeWatch = true
	c.versionWatchStopCh = make(chan struct{})
	c.startServerVersionWatcher()
}

func (c *DynamicDiscoveryManager) StopWatchResourceType() {
	c.watchLock.Lock()
	defer c.watchLock.Unlock()

	if c.aggregatorResourceWatchStopCh == nil {
		return
	}

	close(c.aggregatorResourceWatchStopCh)
	c.aggregatorResourceWatchStopCh = nil

	if c.versionWatchByResurceTypeWatch {
		close(c.versionWatchStopCh)
		c.versionWatchStopCh = nil
		c.versionWatchByResurceTypeWatch = false
	}
}

func (c *DynamicDiscoveryManager) startServerVersionWatcher() {
	if c.stopCh == nil || c.versionWatchStopCh == nil {
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

func (c *DynamicDiscoveryManager) startAggregatorResourceWatcher() {
	if c.stopCh == nil || c.aggregatorResourceWatchStopCh == nil {
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
	go wait.Until(func() {
		_ = c.refetchAggregatorGroups()
	}, 5*time.Minute, stopCh)
}
