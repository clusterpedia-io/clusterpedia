package metrics

import "k8s.io/klog/v2"

var Logger promLogger

type promLogger struct{}

func (pl promLogger) Println(v ...interface{}) {
	klog.Error(v...)
}

func (pl promLogger) Log(v ...interface{}) error {
	klog.Info(v...)
	return nil
}
