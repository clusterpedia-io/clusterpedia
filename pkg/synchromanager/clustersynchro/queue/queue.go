package queue

import "errors"

var ErrQueueClosed = errors.New("queue is closed")

type EventQueue interface {
	Add(obj interface{}) error
	Update(obj interface{}) error
	Delete(obj interface{}) error

	Pop() (*Event, error)
	Done(event *Event) error

	Len() int
	DiscardAndRetain(retain int) bool

	Close()
}
