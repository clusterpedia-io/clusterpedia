package middleware

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

var GlobalSubscriber Subscriber

type Subscriber interface {
	InitSubscriber(stopCh <-chan struct{}) error
	SubscribeTopic(gvr schema.GroupVersionResource, codec runtime.Codec, newFunc func() runtime.Object) error
	EventReceiving(gvr schema.GroupVersionResource, enqueueFunc func(event *watch.Event), clearfunc func()) error
	StopSubscribing(gvr schema.GroupVersionResource) error
	StopSubscriber() error
}
