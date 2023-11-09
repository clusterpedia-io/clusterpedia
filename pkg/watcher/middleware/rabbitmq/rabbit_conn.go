package rabbitmq

import (
	"sync"
	"time"

	"github.com/streadway/amqp"
	"k8s.io/klog/v2"
)

type RabbitConnection struct {
	conn        *amqp.Connection
	url         string
	rw          sync.Mutex
	clientNum   int32
	stopCh      chan struct{}
	notifyClose chan *amqp.Error
}

func NewConn(url string) (*RabbitConnection, error) {
	rabbitConn := &RabbitConnection{
		url:    url,
		rw:     sync.Mutex{},
		stopCh: make(chan struct{}),
	}
	rabbitConn.tryConnect()
	go rabbitConn.loop()

	return rabbitConn, nil
}

func (r *RabbitConnection) NewChannel() (*amqp.Channel, error) {
	r.rw.Lock()
	defer r.rw.Unlock()

	return r.conn.Channel()
}

func (r *RabbitConnection) tryConnect() {
	r.rw.Lock()
	defer r.rw.Unlock()
	if r.conn == nil || r.conn.IsClosed() {
		for {
			conn, err := amqp.Dial(r.url)
			if err != nil {
				klog.Errorf("connect dial error: %v, reconnect after 1 second", err.Error())
				time.Sleep(1 * time.Second)
				continue
			} else {
				r.conn = conn
				r.notifyClose = r.conn.NotifyClose(make(chan *amqp.Error, 1))
				return
			}
		}
	}
}

func (r *RabbitConnection) Close() {
	close(r.stopCh)
}

// loop process channel close event
func (r *RabbitConnection) loop() {
	for {
		select {
		case e := <-r.notifyClose:
			klog.Errorf("connection notifyClose: %v. reconnecting...", e.Error())
			r.tryConnect()
		case <-r.stopCh:
			klog.Info("connection loop check stopped.")
			_ = r.conn.Close()
			return
		}
	}
}
