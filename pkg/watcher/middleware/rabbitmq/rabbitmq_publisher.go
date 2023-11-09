package rabbitmq

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	watchComponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
)

type RabbitmqPublisher struct {
}

func (r *RabbitmqPublisher) InitPublisher(ctx context.Context) error {
	// TODO
	return nil
}

func (r *RabbitmqPublisher) PublishTopic(gvr schema.GroupVersionResource, codec runtime.Codec) error {
	// TODO
	return nil
}

func (r *RabbitmqPublisher) EventSending(gvr schema.GroupVersionResource, startChan func(schema.GroupVersionResource) chan *watchComponents.EventWithCluster,
	publishEvent func(context.Context, *watchComponents.EventWithCluster), GenCrv2Event func(event *watch.Event)) error {
	// TODO
	return nil
}

func (r *RabbitmqPublisher) StopPublishing(gvr schema.GroupVersionResource) error {
	// TODO
	return nil
}

func (r *RabbitmqPublisher) StopPublisher() {
	// TODO
}
