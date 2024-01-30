package components

import (
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

const SIZE = 2000

type EventWithCluster struct {
	Cluster string
	Event   *watch.Event
}

type EventChannels struct {
	channels map[schema.GroupVersionResource]chan *EventWithCluster
	lock     sync.Mutex
}

var EC *EventChannels

func init() {
	EC = &EventChannels{
		channels: make(map[schema.GroupVersionResource]chan *EventWithCluster),
	}
}

func (e *EventChannels) StartChan(gvr schema.GroupVersionResource) chan *EventWithCluster {
	e.lock.Lock()
	defer e.lock.Unlock()

	if _, ok := e.channels[gvr]; !ok {
		e.channels[gvr] = make(chan *EventWithCluster, SIZE)
	}

	return e.channels[gvr]
}

func (e *EventChannels) CloseChannels() {
	e.lock.Lock()
	defer e.lock.Unlock()
	for _, ch := range e.channels {
		close(ch)
	}
}
