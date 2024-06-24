package queue

import (
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

type KeyFunc func(obj interface{}) (string, error)

func NewPressureQueue(keyFunc KeyFunc) *pressurequeue {
	if keyFunc == nil {
		panic("keyFunc is required")
	}

	q := &pressurequeue{
		processing: sets.Set[string]{},
		items:      map[string]*Event{},
		queue:      []string{},
		keyFunc:    keyFunc,
	}
	q.cond.L = &q.lock
	return q
}

type pressurequeue struct {
	lock sync.RWMutex
	cond sync.Cond

	processing sets.Set[string]
	items      map[string]*Event
	queue      []string
	keyFunc    KeyFunc
	closed     bool
}

func (q *pressurequeue) Add(obj interface{}) error {
	q.lock.Lock()
	defer q.lock.Unlock()

	return q.queueActionLocked(Added, obj)
}

func (q *pressurequeue) Update(obj interface{}) error {
	q.lock.Lock()
	defer q.lock.Unlock()

	return q.queueActionLocked(Updated, obj)
}

func (q *pressurequeue) Delete(obj interface{}) error {
	q.lock.Lock()
	defer q.lock.Unlock()

	return q.queueActionLocked(Deleted, obj)
}

func (q *pressurequeue) queueActionLocked(action ActionType, obj interface{}) error {
	key, err := q.keyFunc(obj)
	if err != nil {
		return err
	}
	q.put(key, pressureEvents(q.items[key], &Event{Action: action, Object: obj}))
	return nil
}

func (q *pressurequeue) Reput(event *Event) error {
	if event == nil {
		return nil
	}

	key, err := q.keyFunc(event.Object)
	if err != nil {
		return err
	}

	q.lock.Lock()
	defer q.lock.Unlock()

	q.processing.Delete(key)

	event.reputCount++
	q.put(key, pressureEvents(event, q.items[key]))
	return nil
}

func (q *pressurequeue) put(key string, event *Event) {
	if event == nil {
		return
	}

	if !q.processing.Has(key) {
		if _, existed := q.items[key]; !existed {
			q.queue = append(q.queue, key)
		}
	}
	q.items[key] = event
	q.cond.Broadcast()
}

func (q *pressurequeue) Done(event *Event) error {
	key, err := q.keyFunc(event.Object)
	if err != nil {
		return err
	}

	q.lock.Lock()
	defer q.lock.Unlock()

	q.processing.Delete(key)
	if _, existed := q.items[key]; existed {
		q.queue = append(q.queue, key)
	}
	q.cond.Broadcast()
	return nil
}

func (q *pressurequeue) Pop() (*Event, error) {
	q.lock.Lock()
	defer q.lock.Unlock()

	for {
		for len(q.queue) == 0 {
			if q.closed {
				return nil, ErrQueueClosed
			}
			q.cond.Wait()
		}

		key := q.queue[0]
		q.queue = q.queue[1:]

		event, ok := q.items[key]
		delete(q.items, key)
		if !ok || event == nil {
			// TODO(clusterpedia-io): add log
			continue
		}

		q.processing.Insert(key)
		return event, nil
	}
}

func (q *pressurequeue) PopAll() ([]*Event, error) {
	q.lock.Lock()
	defer q.lock.Unlock()
	if len(q.queue) == 0 {
		if q.closed {
			return nil, ErrQueueClosed
		}
		return []*Event{}, nil
	}

	events := make([]*Event, 0, len(q.queue))
	for _, key := range q.queue {
		if event := q.items[key]; event != nil {
			events = append(events, event)
			q.processing.Insert(key)
		}
	}
	q.items = make(map[string]*Event)
	q.queue = q.queue[:0]
	return events, nil
}

func (q *pressurequeue) Len() int {
	q.lock.Lock()
	defer q.lock.Unlock()
	return len(q.queue)
}

func (q *pressurequeue) DiscardAndRetain(retain int) bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	if len(q.queue) <= retain {
		return false
	}

	q.queue = q.queue[:retain]
	items := make(map[string]*Event, retain)
	for _, key := range q.queue {
		items[key] = q.items[key]
	}
	q.items = items
	return true
}

func (q *pressurequeue) Close() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.closed = true
	q.cond.Broadcast()
}
