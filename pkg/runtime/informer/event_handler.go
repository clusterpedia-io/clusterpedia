package informer

type ResourceEventHandler interface {
	OnAdd(obj interface{}, isInInitialList bool)
	OnUpdate(oldObj, newObj interface{}, isInInitialList bool)
	OnDelete(obj interface{}, isInInitialList bool)
	OnSync(obj interface{})
}

type ResourceEventHandlerFuncs struct {
	AddFunc    func(obj interface{})
	UpdateFunc func(oldObj, newObj interface{})
	DeleteFunc func(obj interface{})
	SyncFunc   func(obj interface{})
}

func (r ResourceEventHandlerFuncs) OnAdd(obj interface{}, _ bool) {
	if r.AddFunc != nil {
		r.AddFunc(obj)
	}
}

func (r ResourceEventHandlerFuncs) OnUpdate(oldObj, newObj interface{}, _ bool) {
	if r.UpdateFunc != nil {
		r.UpdateFunc(oldObj, newObj)
	}
}

func (r ResourceEventHandlerFuncs) OnDelete(obj interface{}) {
	if r.DeleteFunc != nil {
		r.DeleteFunc(obj)
	}
}

func (r ResourceEventHandlerFuncs) OnSync(obj interface{}) {
	if r.SyncFunc != nil {
		r.SyncFunc(obj)
	}
}

type FilteringResourceEventHandler struct {
	FilterFunc func(obj interface{}) bool
	Handler    ResourceEventHandler
}

func (r FilteringResourceEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
	if !r.FilterFunc(obj) {
		return
	}
	r.Handler.OnAdd(obj, isInInitialList)
}

func (r FilteringResourceEventHandler) OnUpdate(oldObj, newObj interface{}, isInInitialList bool) {
	newer := r.FilterFunc(newObj)
	older := r.FilterFunc(oldObj)
	switch {
	case newer && older:
		r.Handler.OnUpdate(oldObj, newObj, isInInitialList)
	case newer && !older:
		r.Handler.OnAdd(newObj, isInInitialList)
	case !newer && older:
		r.Handler.OnDelete(oldObj, isInInitialList)
	default:
		// do nothing
	}
}

func (r FilteringResourceEventHandler) OnDelete(obj interface{}, isInInitialList bool) {
	if !r.FilterFunc(obj) {
		return
	}
	r.Handler.OnDelete(obj, isInInitialList)
}

func (r FilteringResourceEventHandler) OnSync(obj interface{}) {
	if !r.FilterFunc(obj) {
		return
	}
	r.Handler.OnSync(obj)
}
