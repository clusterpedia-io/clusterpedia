package middleware

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	watchComponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
)

var GlobalPublisher Publisher

type Publisher interface {
	InitPublisher(ctx context.Context) error
	PublishTopic(gvr schema.GroupVersionResource, codec runtime.Codec) error
	EventSending(gvr schema.GroupVersionResource, startChan func(schema.GroupVersionResource) chan *watchComponents.EventWithCluster,
		publishEvent func(context.Context, *watchComponents.EventWithCluster), GenCrv2Event func(event *watch.Event)) error
	StopPublishing(gvr schema.GroupVersionResource) error
	StopPublisher()
}
