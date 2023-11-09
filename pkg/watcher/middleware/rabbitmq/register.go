package rabbitmq

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/middleware"
	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/options"
)

const (
	PushlisherName  = "rabbitmq"
	SubscribeerName = "rabbitmq"
)

func NewPulisher(mo *options.MiddlerwareOptions) (middleware.Publisher, error) {
	if mo.MaxConnections <= 0 {
		mo.MaxConnections = 3
	}
	if mo.ExpiresPerSend <= 0 {
		mo.ExpiresPerSend = 150
	}

	url := fmt.Sprintf("amqp://%s:%s@%s:%d/%s", mo.ConnectUser, mo.ConnectPassword, mo.ServerIp, mo.ServerPort, mo.Suffix)
	publisher := &RabbitmqPublisher{
		mqUrl:          url,
		connNums:       mo.MaxConnections,
		producerList:   make(map[schema.GroupVersionResource]*RabbitClient),
		expiresPerSend: mo.ExpiresPerSend,
		rw:             sync.Mutex{},
	}
	return publisher, nil
}

func NewSubscriber(mo *options.MiddlerwareOptions) (middleware.Subscriber, error) {
	if mo.MaxConnections <= 0 {
		mo.MaxConnections = 3
	}

	if mo.QueueExpires <= 0 {
		mo.QueueExpires = 3 * 3600 * 1000 // default 3 hours
	}

	url := fmt.Sprintf("amqp://%s:%s@%s:%d/%s", mo.ConnectUser, mo.ConnectPassword, mo.ServerIp, mo.ServerPort, mo.Suffix)
	subscriber := &RabbitmqSubscriber{
		mqUrl:        url,
		consumerList: make(map[schema.GroupVersionResource]*RabbitClient),
		connNums:     mo.MaxConnections,
		rw:           sync.Mutex{},
		queueExpires: mo.QueueExpires,
	}
	return subscriber, nil
}
