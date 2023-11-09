package components

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

var (
	instance *MultiClusterEventPool
	once     sync.Once
)

type MultiClusterEventPool struct {
	clusterbuffer map[schema.GroupVersionResource]*MultiClusterBuffer

	//todo
	//use atomic.value instead of lock
	lock sync.Mutex
}

func GetMultiClusterEventPool() *MultiClusterEventPool {
	once.Do(func() {
		instance = &MultiClusterEventPool{
			clusterbuffer: map[schema.GroupVersionResource]*MultiClusterBuffer{},
		}
	})
	return instance
}

func (p *MultiClusterEventPool) GetClusterBufferByGVR(gvr schema.GroupVersionResource) *MultiClusterBuffer {
	p.lock.Lock()
	defer p.lock.Unlock()
	if buffer, ok := p.clusterbuffer[gvr]; ok {
		return buffer
	} else {
		wb := newMultiClusterBuffer(gvr)
		p.clusterbuffer[gvr] = wb
		return wb
	}
}

/*func (p *MultiClusterEventPool) RemoveCluster(cluster string) {
	p.lock.Lock()
	defer p.lock.Unlock()
	for _, multiClusterBuffer := range p.clusterbuffer {
		multiClusterBuffer.removeCluster(cluster)
	}
}

func (p *MultiClusterEventPool) RemoveClusterWithGvr(cluster string, gvr schema.GroupVersionResource) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if multiClusterBuffer, ok := p.clusterbuffer[gvr]; ok {
		multiClusterBuffer.removeCluster(cluster)
	}
}*/

type MultiClusterBuffer struct {
	gvr           schema.GroupVersionResource
	watcherbuffer []*MultiClusterWatcher

	lock sync.Mutex
}

func newMultiClusterBuffer(gvr schema.GroupVersionResource) *MultiClusterBuffer {
	wb := &MultiClusterBuffer{
		gvr: gvr,
	}

	return wb
}

func (b *MultiClusterBuffer) ForgetWatcher(watcher *MultiClusterWatcher) {
	b.lock.Lock()
	defer b.lock.Unlock()
	var i int
	hit := false
	for i = 0; i < len(b.watcherbuffer); i++ {
		if b.watcherbuffer[i] == watcher {
			hit = true
			break
		}
	}
	if hit {
		b.watcherbuffer = append(b.watcherbuffer[:i], b.watcherbuffer[i+1:]...)
	}
}

func (b *MultiClusterBuffer) AppendWatcherBuffer(watcher *MultiClusterWatcher) *MultiClusterBuffer {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.watcherbuffer = append(b.watcherbuffer, watcher)

	return b
}

func (b *MultiClusterBuffer) ProcessEvent(obj runtime.Object, eventType watch.EventType) error {
	event := watch.Event{Type: eventType, Object: obj}

	b.lock.Lock()
	defer b.lock.Unlock()

	for _, buffer := range b.watcherbuffer {
		buffer.NonblockingAdd(&event)
	}
	return nil
}

func (b *MultiClusterBuffer) ProcessCompleteEvent(event *watch.Event) error {
	b.lock.Lock()
	defer b.lock.Unlock()

	for _, buffer := range b.watcherbuffer {
		buffer.NonblockingAdd(event)
	}
	return nil
}

/*func (b *MultiClusterBuffer) UpdateObjectResourceVersion(obj runtime.Object, clusterName string) (*watchcache.ClusterResourceVersion, error) {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	// clusterpedia will retry send event when storage db failed. in this case, rv has already been encoded
	if isCrv(metaobj.GetResourceVersion()) {
		return watchcache.NewClusterResourceVersionFromString(metaobj.GetResourceVersion())
	} else {
		return b.resourceVersionSynchro.UpdateClusterResourceVersion(obj, clusterName)
	}
}

func isCrv(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err != nil
}*/

/*func (b *MultiClusterBuffer) UpdateObjectResourceVersion(obj runtime.Object, clusterName string) ([]byte, error) {
	return b.resourceVersionSynchro.UpdateClusterResourceVersionWithBytes(obj, clusterName)
}

func (b *MultiClusterBuffer) removeCluster(cluster string) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.resourceVersionSynchro.RemoveCluster(cluster)
}*/
