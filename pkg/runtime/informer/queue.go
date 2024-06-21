package informer

import (
	"k8s.io/client-go/tools/cache"
)

type ExtraStore interface {
	// Add adds the given object to the accumulator associated with the given object's key
	Add(obj interface{}) error

	// Update updates the given object in the accumulator associated with the given object's key
	Update(obj interface{}) error

	// Delete deletes the given object from the accumulator associated with the given object's key
	Delete(obj interface{}) error

	// Replace will delete the contents of the store, using instead the
	// given list. Store takes ownership of the list, you should not reference
	// it after calling this function.
	Replace([]interface{}, string) error
}

type queueWithExtraStore struct {
	cache.Queue
	extra ExtraStore
}

var _ cache.Queue = &queueWithExtraStore{}

func (queue *queueWithExtraStore) Add(obj interface{}) error {
	_ = queue.extra.Add(obj)
	return queue.Queue.Add(obj)
}

func (queue *queueWithExtraStore) Update(obj interface{}) error {
	_ = queue.extra.Update(obj)
	return queue.Queue.Update(obj)
}

func (queue *queueWithExtraStore) Delete(obj interface{}) error {
	_ = queue.extra.Delete(obj)
	return queue.Queue.Delete(obj)
}

func (queue *queueWithExtraStore) Replace(list []interface{}, rv string) error {
	_ = queue.extra.Replace(list, rv)
	return queue.Queue.Replace(list, rv)
}
