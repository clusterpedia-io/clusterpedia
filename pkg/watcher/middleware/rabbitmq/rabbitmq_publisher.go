package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	watchcomponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
)

type RabbitmqPublisher struct {
	connNums        int    // tcp connects
	mqUrl           string // rabbitmq addr
	ctx             context.Context
	connectionsPool []*RabbitConnection
	producerList    map[schema.GroupVersionResource]*RabbitClient
	stopCh          <-chan struct{}
	rw              sync.Mutex
	expiresPerSend  int
}

func (r *RabbitmqPublisher) InitPublisher(ctx context.Context) error {
	r.ctx = ctx
	r.stopCh = ctx.Done()
	return r.initConnectionsPool()
}

func (r *RabbitmqPublisher) initConnectionsPool() (err error) {
	for i := len(r.connectionsPool); i < r.connNums; i++ {
		conn, err := NewConn(r.mqUrl)
		if err != nil {
			klog.Error("init connect error: ", err.Error())
			return err
		}
		klog.Info("connect-%d succes", i)
		r.connectionsPool = append(r.connectionsPool, conn)
	}
	return
}

func (r *RabbitmqPublisher) PublishTopic(gvr schema.GroupVersionResource, codec runtime.Codec) error {
	gvrStr := GvrString(gvr)
	r.rw.Lock()
	defer r.rw.Unlock()
	if _, ok := r.producerList[gvr]; !ok {
		klog.Infof("publish topic. gvr: %s. producer size: %d", gvrStr, len(r.producerList))
		queueEx := QueueExchange{
			RoutingKey:   gvrStr,
			ExchangeName: gvrStr,
			ExchangeType: "direct",
		}

		conn, err := r.assignConnByBalancePolicy()
		if err != nil {
			return fmt.Errorf("assign connection to producer failed. %v", err.Error())
		}
		producer := NewProducer(queueEx, conn, codec, r.expiresPerSend, r.stopCh)
		atomic.AddInt32(&conn.clientNum, 1)
		r.producerList[gvr] = producer
	}
	return nil
}

func (r *RabbitmqPublisher) EventSending(gvr schema.GroupVersionResource, startChan func(schema.GroupVersionResource) chan *watchcomponents.EventWithCluster,
	publishEvent func(context.Context, *watchcomponents.EventWithCluster), genCrv2Event func(event *watch.Event)) error {
	r.rw.Lock()
	defer r.rw.Unlock()
	if _, ok := r.producerList[gvr]; !ok {
		return fmt.Errorf("producer not found. this should not happen normally")
	}
	p := r.producerList[gvr]
	if !p.started {
		p.started = true
		go p.Produce(startChan(gvr), publishEvent, r.ctx, genCrv2Event)
	}
	return nil
}

func (r *RabbitmqPublisher) StopPublishing(gvr schema.GroupVersionResource) error {
	r.rw.Lock()
	defer r.rw.Unlock()
	if producer, ok := r.producerList[gvr]; ok {
		_ = producer.Destroy()
		delete(r.producerList, gvr)
		atomic.AddInt32(&producer.conn.clientNum, -1)
	}
	return nil
}

func (r *RabbitmqPublisher) StopPublisher() {
	klog.Warning("stop publisher... this may caused by leader changed")
	for gvr := range r.producerList {
		_ = r.StopPublishing(gvr)
	}
	for _, conn := range r.connectionsPool {
		conn.Close()
	}
	r.producerList = make(map[schema.GroupVersionResource]*RabbitClient)
	r.connectionsPool = nil
}

func (r *RabbitmqPublisher) assignConnByBalancePolicy() (*RabbitConnection, error) {
	// rabbitmq needs to be ready before clusterpedia starts
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
