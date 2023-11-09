package rabbitmq

import (
	"fmt"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

type RabbitmqSubscriber struct {
	connNums        int
	mqUrl           string
	connectionsPool []*RabbitConnection
	consumerList    map[schema.GroupVersionResource]*RabbitClient
	stopCh          <-chan struct{}
	rw              sync.Mutex
	queueExpires    int64
}

func (r *RabbitmqSubscriber) InitSubscriber(stopCh <-chan struct{}) error {
	r.stopCh = stopCh
	return r.initConnectionsPool()
}

func (r *RabbitmqSubscriber) initConnectionsPool() (err error) {
	for i := len(r.connectionsPool); i < r.connNums; i++ {
		conn, err := NewConn(r.mqUrl)
		if err != nil {
			klog.Error("init connect error: %v", err.Error())
			return err
		}
		klog.Infof("connect-%d succes", i)
		r.connectionsPool = append(r.connectionsPool, conn)
	}
	return
}

func (r *RabbitmqSubscriber) SubscribeTopic(gvr schema.GroupVersionResource, codec runtime.Codec, newFunc func() runtime.Object) error {
	gvrStr := GvrString(gvr)
	r.rw.Lock()
	defer r.rw.Unlock()
	if _, ok := r.consumerList[gvr]; !ok {
		conn, err := r.assignConnByBalancePolicy()
		if err != nil {
			return fmt.Errorf("assign connection to consumer failed. %v", err.Error())
		}
		queueEx := QueueExchange{
			QueueName:    NewQueue(gvrStr, conn, r.queueExpires),
			RoutingKey:   gvrStr,
			ExchangeName: gvrStr,
			ExchangeType: "direct",
		}
		consumer := NewConsumer(queueEx, conn, codec, r.stopCh, newFunc, r.queueExpires)
		atomic.AddInt32(&conn.clientNum, 1)
		r.consumerList[gvr] = consumer
		klog.V(2).Infof("create new consumer for gvr: %v. consumer list size: %d", gvr, len(r.consumerList))
	}
	return nil
}

func (r *RabbitmqSubscriber) EventReceiving(gvr schema.GroupVersionResource, enqueueFunc func(event *watch.Event), clearfunc func()) error {
	r.rw.Lock()
	defer r.rw.Unlock()
	if _, ok := r.consumerList[gvr]; !ok {
		return fmt.Errorf("consumer not found. this should not happen normally")
	}
	consumer := r.consumerList[gvr]
	if !consumer.started {
		consumer.started = true
		go consumer.Consume(enqueueFunc, clearfunc)
	}
	return nil
}

func (r *RabbitmqSubscriber) StopSubscribing(gvr schema.GroupVersionResource) error {
	r.rw.Lock()
	defer r.rw.Unlock()
	if consumer, ok := r.consumerList[gvr]; ok {
		_ = consumer.Destroy()
		delete(r.consumerList, gvr)
		atomic.AddInt32(&consumer.conn.clientNum, -1)
	}
	return nil
}

func (r *RabbitmqSubscriber) StopSubscriber() error {
	r.rw.Lock()
	defer r.rw.Unlock()
	for gvr, consumer := range r.consumerList {
		_ = consumer.Destroy()
		delete(r.consumerList, gvr)
		atomic.AddInt32(&consumer.conn.clientNum, -1)
	}
	for _, conn := range r.connectionsPool {
		conn.Close()
	}
	r.consumerList = make(map[schema.GroupVersionResource]*RabbitClient)
	r.connectionsPool = nil
	return nil
}

func (r *RabbitmqSubscriber) assignConnByBalancePolicy() (*RabbitConnection, error) {
	if len(r.connectionsPool) != r.connNums {
		err := r.initConnectionsPool()
		if err != nil {
			return nil, err
		}
	}

	conn := r.connectionsPool[0]
	for i := 1; i < r.connNums; i++ {
		if r.connectionsPool[i].clientNum < conn.clientNum {
			conn = r.connectionsPool[i]
		}
	}
	return conn, nil
}
