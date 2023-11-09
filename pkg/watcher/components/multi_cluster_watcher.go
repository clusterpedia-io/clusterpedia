package components

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme"
)

type FilterWithAttrsFunc func(key string, l labels.Set, f fields.Set) bool

// CacheWatcaher implements watch.Interface
type MultiClusterWatcher struct {
	input chan *watch.Event
	//output
	result  chan watch.Event
	done    chan struct{}
	stopped bool
	forget  func()
	filter  FilterWithAttrsFunc
	keyFunc func(obj runtime.Object) (string, error)
	gvk     schema.GroupVersionKind
}

func NewMultiClusterWatcher(chanSize int, filter FilterWithAttrsFunc, keyFunc func(obj runtime.Object) (string, error), gvk schema.GroupVersionKind) *MultiClusterWatcher {
	return &MultiClusterWatcher{
		input:   make(chan *watch.Event, chanSize),
		result:  make(chan watch.Event, chanSize),
		done:    make(chan struct{}),
		stopped: false,
		forget:  func() {},
		filter:  filter,
		keyFunc: keyFunc,
		gvk:     gvk,
	}
}

func (w *MultiClusterWatcher) SetForget(forget func()) {
	if forget != nil {
		w.forget = forget
	}
}

// ResultChan implements watch.Interface.
func (w *MultiClusterWatcher) ResultChan() <-chan watch.Event {
	return w.result
}

// Stop implements watch.Interface.
func (w *MultiClusterWatcher) Stop() {
	w.forget()
}

func (w *MultiClusterWatcher) StopThreadUnsafe() {
	if !w.stopped {
		w.stopped = true
		close(w.done)
		close(w.input)
	}
}

func (w *MultiClusterWatcher) NonblockingAdd(event *watch.Event) bool {
	select {
	case w.input <- event:
		klog.V(8).Infof("Event in to input %v : %v \n", event.Type, event.Object.GetObjectKind().GroupVersionKind())
		return true
	default:
		return false
	}
}

// Nil timer means that add will not block (if it can't send event immediately, it will break the watcher)
func (w *MultiClusterWatcher) Add(event *watch.Event, timer *time.Timer) bool {
	// Try to send the event immediately, without blocking.
	if w.NonblockingAdd(event) {
		return true
	}

	closeFunc := func() {
		// This means that we couldn't send event to that watcher.
		// Since we don't want to block on it infinitely,
		// we simply terminate it.
		//klog.V(1).Infof("Forcing watcher close due to unresponsiveness: %v", c.objectType.String())
		w.forget()
	}

	if timer == nil {
		closeFunc()
		return false
	}

	// OK, block sending, but only until timer fires.
	select {
	case w.input <- event:
		return true
	case <-timer.C:
		closeFunc()
		return false
	}
}

func getNestedString(obj map[string]interface{}, fields ...string) string {
	val, found, err := unstructured.NestedString(obj, fields...)
	if !found || err != nil {
		return ""
	}
	return val
}

func getNamespace(u *unstructured.Unstructured) string {
	namespace := getNestedString(u.Object, "metadata", "namespace")
	if namespace == "" {
		namespace = getNestedString(u.Object, "objectMeta", "namespace")
	}
	return namespace
}

func getName(u *unstructured.Unstructured) string {
	namespace := getNestedString(u.Object, "metadata", "name")
	if namespace == "" {
		namespace = getNestedString(u.Object, "objectMeta", "name")
	}
	return namespace
}

func internalToUnstructured(internal runtime.Object, gvk schema.GroupVersionKind) (*unstructured.Unstructured, error) {
	var into runtime.Object
	var err error
	if resourcescheme.LegacyResourceScheme.IsGroupRegistered(gvk.Group) {
		into, _ = resourcescheme.LegacyResourceScheme.New(gvk)
		err = resourcescheme.LegacyResourceScheme.Convert(internal, into, nil)
	} else {
		into, _ = resourcescheme.UnstructuredScheme.New(gvk)
		err = resourcescheme.UnstructuredScheme.Convert(internal, into, nil)
	}
	if err != nil {
		return nil, err
	}
	unstructured, err := externalToUnstructured(into)
	if err != nil {
		return nil, err
	}
	return unstructured, nil
}

func externalToUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	uncastObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: uncastObj}, nil
}

// apiserver\pkg\storage\cacher\cacher.go:1339  这里区别比较大 估计会有bug
func (w *MultiClusterWatcher) convertToWatchEvent(event *watch.Event) *watch.Event {
	if event.Type == watch.Error || w.filter == nil {
		return event
	}

	// defer func() {
	// 	if err := recover(); err != nil {
	// 		klog.Error(err)
	// 	}
	// }()

	unstructuredData, err := internalToUnstructured(event.Object, w.gvk)
	if err != nil {
		klog.Error(err)
		return nil
	}
	key, err := w.keyFunc(event.Object)
	if err != nil {
		klog.Error(err)
		return nil
	}
	curObjPasses := w.filter(key, unstructuredData.GetLabels(), fields.Set{
		"metadata.name":      getName(unstructuredData),
		"metadata.namespace": getNamespace(unstructuredData),
	})
	if curObjPasses {
		return event
	}
	return nil
}

func (w *MultiClusterWatcher) sendWatchCacheEvent(event *watch.Event) {
	watchEvent := w.convertToWatchEvent(event)
	if watchEvent == nil {
		// Watcher is not interested in that object.
		return
	}

	// We need to ensure that if we put event X to the c.result, all
	// previous events were already put into it before, no matter whether
	// c.done is close or not.
	// Thus we cannot simply select from c.done and c.result and this
	// would give us non-determinism.
	// At the same time, we don't want to block infinitely on putting
	// to c.result, when c.done is already closed.

	// This ensures that with c.done already close, we at most once go
	// into the next select after this. With that, no matter which
	// statement we choose there, we will deliver only consecutive
	// events.
	select {
	case <-w.done:
		return
	default:
	}

	select {
	case w.result <- *watchEvent:
	case <-w.done:
	}
}

// Process send the events which stored in watchCache into the result channel,and select the event from input channel into result channel continuously.
func (w *MultiClusterWatcher) Process(ctx context.Context, initEvents []*watch.Event) {
	defer utilruntime.HandleCrash()

	for _, event := range initEvents {
		w.sendWatchCacheEvent(event)
	}

	defer close(w.result)
	defer w.Stop()
	for {
		select {
		case event, ok := <-w.input:
			if !ok {
				return
			}
			w.sendWatchCacheEvent(event)

		case <-ctx.Done():
			return
		}
	}
}
