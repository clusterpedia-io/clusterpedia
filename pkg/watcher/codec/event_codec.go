package codec

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

const EVENTTYPE = "EventTypeForLabel"

func EventEncode(eventType watch.EventType, obj runtime.Object, codec runtime.Codec) ([]byte, error) {
	accessor := meta.NewAccessor()
	labels, err := accessor.Labels(obj)
	if err != nil {
		return nil, err
	}
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[EVENTTYPE] = string(eventType)
	err = accessor.SetLabels(obj, labels)
	if err != nil {
		return nil, err
	}

	var buffer bytes.Buffer
	if err := codec.Encode(obj, &buffer); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func EventDecode(value []byte, codec runtime.Codec, newFunc func() runtime.Object) (*watch.Event, error) {
	into := newFunc()
	obj, _, err := codec.Decode(value, nil, into)
	if err != nil {
		return nil, err
	}

	accessor := meta.NewAccessor()
	labels, err := accessor.Labels(obj)
	if err != nil {
		return nil, err
	}
	var eventType string
	if labels != nil {
		eventType = labels[EVENTTYPE]
		delete(labels, EVENTTYPE)
		if eventType == "" {
			return nil, fmt.Errorf("event can not find eventtype")
		}
	}
	err = accessor.SetLabels(obj, labels)
	if err != nil {
		return nil, err
	}

	return &watch.Event{
		Type:   watch.EventType(eventType),
		Object: obj,
	}, nil
}
