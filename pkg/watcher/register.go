package watcher

import (
	"fmt"

	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/middleware"
	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/middleware/rabbitmq"
	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/options"
)

type NewPublisherFunc func(mo *options.MiddlerwareOptions) (middleware.Publisher, error)
type NewSubscriberFunc func(mo *options.MiddlerwareOptions) (middleware.Subscriber, error)

var publisherFuncs = make(map[string]NewPublisherFunc)
var subscriberFuncs = make(map[string]NewSubscriberFunc)

func init() {
	RegisterPublisherFunc(rabbitmq.PushlisherName, rabbitmq.NewPulisher)
	RegisterSubscriberFunc(rabbitmq.SubscribeerName, rabbitmq.NewSubscriber)
}

func RegisterPublisherFunc(name string, f NewPublisherFunc) {
	if _, ok := publisherFuncs[name]; ok {
		panic(fmt.Sprintf("publisher %s has been registered", name))
	}
	publisherFuncs[name] = f
}

func RegisterSubscriberFunc(name string, f NewSubscriberFunc) {
	if _, ok := subscriberFuncs[name]; ok {
		panic(fmt.Sprintf("subscriber %s has been registered", name))
	}
	subscriberFuncs[name] = f
}

func NewPulisher(mo *options.MiddlerwareOptions) error {
	provider, ok := publisherFuncs[mo.Name]
	if !ok {
		return fmt.Errorf("publisher %s is unregistered", mo.Name)
	}

	publisher, err := provider(mo)
	if err != nil {
		return fmt.Errorf("Failed to init middleware: %w", err)
	}
	middleware.GlobalPublisher = publisher

	return nil
}

func NewSubscriber(mo *options.MiddlerwareOptions) error {
	provider, ok := subscriberFuncs[mo.Name]
	if !ok {
		return fmt.Errorf("publisher %s is unregistered", mo.Name)
	}

	subscriber, err := provider(mo)
	if err != nil {
		return fmt.Errorf("Failed to init middleware: %w", err)
	}
	middleware.GlobalSubscriber = subscriber

	return nil
}
