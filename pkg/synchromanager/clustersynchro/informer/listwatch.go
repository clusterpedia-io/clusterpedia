package informer

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

type TweakListOptionsFunc func(*metav1.ListOptions)

type DynamicListerWatcherFactory interface {
	ForResource(namespace string, gvr schema.GroupVersionResource) cache.ListerWatcher
	ForResourceWithOptions(namespace string, gvr schema.GroupVersionResource, optionsFunc TweakListOptionsFunc) cache.ListerWatcher
}

func NewDynamicListerWatcherFactory(config *rest.Config) (DynamicListerWatcherFactory, error) {
	// check config
	if _, err := dynamic.NewForConfig(config); err != nil {
		return nil, err
	}

	return &listerWatcherFactory{config}, nil
}

type listerWatcherFactory struct {
	config *rest.Config
}

func (f *listerWatcherFactory) ForResource(namespace string, gvr schema.GroupVersionResource) cache.ListerWatcher {
	client := dynamic.NewForConfigOrDie(f.config)
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), options)
		},
	}
}

func (f *listerWatcherFactory) ForResourceWithOptions(namespace string, gvr schema.GroupVersionResource, tweakListOptions TweakListOptionsFunc) cache.ListerWatcher {
	client := dynamic.NewForConfigOrDie(f.config)
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			if tweakListOptions != nil {
				tweakListOptions(&options)
			}
			return client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			if tweakListOptions != nil {
				tweakListOptions(&options)
			}
			return client.Resource(gvr).Namespace(namespace).Watch(context.TODO(), options)
		},
	}
}
