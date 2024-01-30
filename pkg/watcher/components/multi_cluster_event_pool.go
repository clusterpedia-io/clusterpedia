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
