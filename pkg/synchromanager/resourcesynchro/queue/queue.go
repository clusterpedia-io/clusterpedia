package queue

import "errors"

var ErrQueueClosed = errors.New("queue is closed")

type EventQueue interface {
	Add(obj interface{}, isInInitialList bool) error
	Update(obj interface{}, isInInitialList bool) error
	Delete(obj interface{}, isInInitialList bool) error

	Pop() (*Event, error)
	Done(event *Event) error

	Len() int
	DiscardAndRetain(retain int) bool
	HasInitialEvents() bool

	Close()
}
