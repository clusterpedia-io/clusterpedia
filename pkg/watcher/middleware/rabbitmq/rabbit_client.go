package rabbitmq

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/streadway/amqp"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/watcher/codec"
	watchcomponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
)

const (
	RoleConsumer = "consumer"
	RoleProducer = "producer"
)

type QueueExchange struct {
	QueueName    string
	RoutingKey   string
	ExchangeName string
	ExchangeType string
}

type RabbitClient struct {
	QueueExchange
	conn           *RabbitConnection
	channel        *amqp.Channel
	codec          runtime.Codec
	started        bool
	cliStopCh      chan bool
	globalStopCh   <-chan struct{}
	expiresPerSend int
	notifyConfirm  chan amqp.Confirmation // msg send confirmed chan
	notifyClose    chan *amqp.Error       // channel closed chan
	role           string
	newFunc        func() runtime.Object // event decode
	queueExpires   int64
}

func NewProducer(queueEx QueueExchange, conn *RabbitConnection, codec runtime.Codec, expiresPerSend int, gStopCh <-chan struct{}) *RabbitClient {
	return &RabbitClient{
		QueueExchange:  queueEx,
		conn:           conn,
		codec:          codec,
		cliStopCh:      make(chan bool, 1),
		globalStopCh:   gStopCh,
		expiresPerSend: expiresPerSend,
		role:           RoleProducer,
	}
}

func NewConsumer(queueEx QueueExchange, conn *RabbitConnection, codec runtime.Codec, gStopCh <-chan struct{}, newFunc func() runtime.Object, queueExpires int64) *RabbitClient {
	return &RabbitClient{
		QueueExchange: queueEx,
		conn:          conn,
		codec:         codec,
		cliStopCh:     make(chan bool, 1),
		globalStopCh:  gStopCh,
		role:          RoleConsumer,
		newFunc:       newFunc,
		queueExpires:  queueExpires,
	}
}

func NewQueue(quePrefix string, conn *RabbitConnection, queueExpires int64) string {
	for {
		ch := CreateChannel(conn)

		randStr := fmt.Sprintf("%d%d%d%d", rand.Intn(10), rand.Intn(10), rand.Intn(10), rand.Intn(10))
		timeStr := time.Now().Format("2006-01-02 15-04-05")
		timeStr = strings.ReplaceAll(timeStr, "-", "")
		timeStr = strings.ReplaceAll(timeStr, " ", "")
		queue := fmt.Sprintf("%s_%s_%s", quePrefix, timeStr, randStr)
		args := make(amqp.Table, 1)
		args["x-expires"] = queueExpires
		_, err := ch.QueueDeclarePassive(queue, true, false, false, false, args)
		if err == nil { // queue already exist
			_ = ch.Close()
			continue
		} else { // declare the queue
			_ = ch.Close()
			ch := CreateChannel(conn)
			_, err = ch.QueueDeclare(queue, true, false, false, false, args)
			if err != nil {
				klog.Errorf("rabbitmq queueDeclare failed: %v", err)
				_ = ch.Close()
				continue
			} else {
				_ = ch.Close()
				return queue
			}
		}
	}
}

// CreateChannel open a channel until success
func CreateChannel(conn *RabbitConnection) *amqp.Channel {
	for {
		conn.tryConnect()
		ch, err := conn.NewChannel()
		if err != nil {
			klog.Error("open channel failed. ", err, ". retry after 1 second")
			time.Sleep(1 * time.Second)
			continue
		} else {
			return ch
		}
	}
}

func (r *RabbitClient) Destroy() (err error) {
	r.cliStopCh <- true
	return nil
}

func (r *RabbitClient) DestroyGvr() {
	klog.Info("consume stopped for client stop cmd. delete queue: ", r.QueueName)
	_, err := r.channel.QueueDelete(r.QueueName, false, false, true)
	if err != nil {
		klog.Errorf("delete %s queue fail. %v", r.QueueName, err.Error())
	} else {
		klog.Info("deleted queue ", r.QueueName)
	}
	_ = r.closeChannel()
}

func (r *RabbitClient) initChannel() {
	for {
		r.channel = CreateChannel(r.conn)
		err := r.initQuExchange()
		if err != nil {
			klog.Error("init channel failed. ", err.Error())
			_ = r.closeChannel()
			continue
		} else {
			return
		}
	}
}

func (r *RabbitClient) initQuExchange() error {
	args := make(amqp.Table, 1)
	args["x-expires"] = r.queueExpires
	err := r.channel.ExchangeDeclare(r.ExchangeName, r.ExchangeType, true, false, false, false, args)
	if err != nil {
		return fmt.Errorf("rabbitmq exchangeDeclare failed: %v", err)
	}

	r.notifyClose = r.channel.NotifyClose(make(chan *amqp.Error, 1)) // listen channel close event

	if r.role == RoleProducer {
		err = r.channel.Confirm(false) // set msg confirm mode
		if err != nil {
			return fmt.Errorf("rabbitmq confirm error. %v", err)
		}
		r.notifyConfirm = r.channel.NotifyPublish(make(chan amqp.Confirmation, 1))
	} else {
		_, err = r.channel.QueueDeclare(r.QueueName, true, false, false, false, args)
		if err != nil {
			return fmt.Errorf("rabbitmq queueDeclare failed: %v", err)
		}

		err = r.channel.QueueBind(r.QueueName, r.RoutingKey, r.ExchangeName, false, nil)
		if err != nil {
			return fmt.Errorf("rabbitmq queueBind failed: %v", err)
		}

		err = r.channel.Qos(1, 0, false)
		if err != nil {
			return fmt.Errorf("rabbitmq Qos failed: %v", err)
		}
	}
	return nil
}

func (r *RabbitClient) closeChannel() (err error) {
	r.channel.Close()
	if err != nil {
		return fmt.Errorf("close rabbitmq channel failed: %v", err)
	}
	return
}

// sendEventSynchro send message until success
func (r *RabbitClient) sendEventSynchro(event *watch.Event, expiresPerTry int) error {
	msgBytes, err := codec.EventEncode(event.Type, event.Object, r.codec)
	if err != nil {
		return fmt.Errorf("event encode failed. error: %v", err.Error())
	}
	ticker := time.NewTicker(time.Duration(expiresPerTry) * time.Second)
	defer ticker.Stop()
	for {
		_ = r.channel.Publish(
			r.ExchangeName,
			r.RoutingKey,
			false,
			false,
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        msgBytes,
			})

		select {
		case c := <-r.notifyConfirm:
			if !c.Ack {
				klog.Errorf("rabbit confirm ack false. retry init channel and send. exchange: %s", r.ExchangeName)
			} else {
				return nil
			}
		case <-ticker.C:
			klog.Errorf("send event timeout. retry init channel and send. exchange: %s", r.ExchangeName)
		}

		_ = r.closeChannel()
		r.initChannel()
	}
}

func (r *RabbitClient) Produce(eventChan chan *watchcomponents.EventWithCluster, publishEvent func(context.Context, *watchcomponents.EventWithCluster),
	ctx context.Context, genCrv2Event func(event *watch.Event)) {
	for {
		r.initChannel()

	LOOP:
		for {
			select {
			case e := <-r.notifyClose:
				klog.Warningf("channel notifyClose: %v. exchange: %s. retry channel connecting", e.Error(), r.ExchangeName)
				break LOOP
			case event := <-eventChan:
				genCrv2Event(event.Event)
				err := r.sendEventSynchro(event.Event, r.expiresPerSend)
				if err != nil {
					klog.Errorf("send event error %v. exchange: %s. this should not happen normally", err.Error(), r.ExchangeName)
				} else {
					publishEvent(ctx, event)
				}
			case <-r.cliStopCh:
				klog.Info("produce stopped for client stop cmd. exchange: ", r.ExchangeName)
				_ = r.closeChannel()
				close(r.cliStopCh)
				return
			case <-r.globalStopCh:
				klog.Info("produce stopped for global publisher stopped. exchange: ", r.ExchangeName)
				_ = r.closeChannel()
				return
			}
		}
	}
}

func (r *RabbitClient) Consume(enqueueFunc func(event *watch.Event), clearfunc func()) {
	for {
		r.initChannel()
		msgList, err := r.channel.Consume(r.QueueName, "", false, false, false, false, nil)
		if err != nil {
			klog.Errorf("consume err: ", err.Error())
			_ = r.closeChannel()
			continue
		}

	LOOP:
		for {
			select {
			case <-r.cliStopCh:
				klog.Info("consume stopped for client stop cmd. delete queue: ", r.QueueName)
				_, err = r.channel.QueueDelete(r.QueueName, false, false, true)
				if err != nil {
					klog.Errorf("delete %s queue fail. %v", r.QueueName, err.Error())
				} else {
					klog.Info("deleted queue ", r.QueueName)
				}
				_ = r.closeChannel()
				close(r.cliStopCh)
				return
			case msg := <-msgList:
				//处理数据
				event, _ := codec.EventDecode(msg.Body, r.codec, r.newFunc)
				klog.V(7).Infof("Event in to cache %v : %v \n", event.Type, event.Object.GetObjectKind().GroupVersionKind())
				err = msg.Ack(true)
				if err != nil {
					klog.Errorf("msg ack error: %v. event: %v, queue: %s. retry init channel and consume...", err.Error(), event.Type, r.QueueName)
					break LOOP
				}
				enqueueFunc(event)
			case e := <-r.notifyClose:
				klog.Warningf("channel notifyClose: %v. queue: %s. retry channel connecting", e.Error(), r.QueueName)
				break LOOP
			case <-r.globalStopCh:
				klog.Info("consume stopped for global publisher stopped. delete queue: ", r.QueueName)
				_, err = r.channel.QueueDelete(r.QueueName, false, false, true)
				if err != nil {
					klog.Errorf("delete %s queue fail. %v", r.QueueName, err.Error())
				} else {
					klog.Info("deleted queue ", r.QueueName)
				}
				_ = r.closeChannel()
				return
			}
		}
	}
}

func GvrString(gvr schema.GroupVersionResource) string {
	group := strings.ReplaceAll(gvr.Group, ".", "_")
	return fmt.Sprintf("%s_%s_%s", group, gvr.Version, gvr.Resource)
}
