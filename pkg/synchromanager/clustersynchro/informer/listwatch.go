package informer

import (
	"context"
	"math/rand"
	"time"

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

var defaultMinWatchTimeout = 15 * time.Minute

func NewDynamicListerWatcherFactory(config *rest.Config) (DynamicListerWatcherFactory, error) {
	// check config
	if _, err := dynamic.NewForConfig(config); err != nil {
		return nil, err
	}

	// TODO(iceber): support for setting `minWatchTimeout`
	return &listerWatcherFactory{
		config:          config,
		minWatchTimeout: defaultMinWatchTimeout,
	}, nil
}

type listerWatcherFactory struct {
	config          *rest.Config
	minWatchTimeout time.Duration
}

func (f *listerWatcherFactory) ForResource(namespace string, gvr schema.GroupVersionResource) cache.ListerWatcher {
	client := dynamic.NewForConfigOrDie(f.config)
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return client.Resource(gvr).Namespace(namespace).List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			// the minWatchTimeout for reflector is [5m, 10m],
			// set to [f.minWatchTimeout, 2 * f.minWatchTimeout].
			timeoutSeconds := int64(f.minWatchTimeout.Seconds() * (rand.Float64() + 1.0))
			options.TimeoutSeconds = &timeoutSeconds
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
