package informer

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

type TweakListOptionsFunc func(*metav1.ListOptions)

type DynamicListerWatcherFactory interface {
	ForResource(namespace string, gvr schema.GroupVersionResource) cache.ListerWatcher
	ForResourceWithOptions(namespace string, gvr schema.GroupVersionResource, optionsFunc TweakListOptionsFunc) cache.ListerWatcher
}

func NewDynamicListWatcherFactory(client dynamic.Interface) DynamicListerWatcherFactory {
	return &listerWatcherFactory{client}
}

type listerWatcherFactory struct {
	client dynamic.Interface
}

func (f *listerWatcherFactory) ForResource(namespace string, gvr schema.GroupVersionResource) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return f.client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return f.client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), options)
		},
	}
}

func (f *listerWatcherFactory) ForResourceWithOptions(namespace string, gvr schema.GroupVersionResource, tweakListOptions TweakListOptionsFunc) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			if tweakListOptions != nil {
				tweakListOptions(&options)
			}
			return f.client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			if tweakListOptions != nil {
				tweakListOptions(&options)
			}
			return f.client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), options)
		},
	}
}
