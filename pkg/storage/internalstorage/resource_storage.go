package internalstorage

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	genericstorage "k8s.io/apiserver/pkg/storage"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
	watchutil "github.com/clusterpedia-io/clusterpedia/pkg/utils/watch"
	watchcomponents "github.com/clusterpedia-io/clusterpedia/pkg/watcher/components"
)

type ResourceStorage struct {
	db    *gorm.DB
	codec runtime.Codec

	storageGroupResource schema.GroupResource
	storageVersion       schema.GroupVersion
	memoryVersion        schema.GroupVersion

	buffer    *watchcomponents.MultiClusterBuffer
	watchLock sync.Mutex

	eventCache *watchcomponents.EventCache
	Namespaced bool

	eventChan chan *watchcomponents.EventWithCluster

	newFunc func() runtime.Object

	KeyFunc func(runtime.Object) (string, error)
}

func (s *ResourceStorage) GetStorageConfig() *storage.ResourceStorageConfig {
	return &storage.ResourceStorageConfig{
		Codec:                s.codec,
		StorageGroupResource: s.storageGroupResource,
		StorageVersion:       s.storageVersion,
		MemoryVersion:        s.memoryVersion,
	}
}

func (s *ResourceStorage) Create(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" {
		return fmt.Errorf("%s: kind is required", gvk)
	}

	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	//deleted object could be created again
	condition := map[string]interface{}{
		"namespace": metaobj.GetNamespace(),
		"name":      metaobj.GetName(),
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"deleted":   true,
	}
	if cluster != "" {
		condition["cluster"] = cluster
	}
	dbResult := s.db.Model(&Resource{}).Where(condition).Delete(&Resource{})
	if dbResult.Error != nil {
		err = InterpretResourceDBError(cluster, metaobj.GetName(), dbResult.Error)
		return fmt.Errorf("[Create]:  Object %s/%s has been created failed in step one, err: %v", metaobj.GetName(), metaobj.GetNamespace(), err)
	}

	var ownerUID types.UID
	if owner := metav1.GetControllerOfNoCopy(metaobj); owner != nil {
		ownerUID = owner.UID
	}

	var buffer bytes.Buffer
	if err := s.codec.Encode(obj, &buffer); err != nil {
		return err
	}

	resource := Resource{
		Cluster:                cluster,
		OwnerUID:               ownerUID,
		UID:                    metaobj.GetUID(),
		Name:                   metaobj.GetName(),
		Namespace:              metaobj.GetNamespace(),
		Group:                  s.storageGroupResource.Group,
		Resource:               s.storageGroupResource.Resource,
		Version:                s.storageVersion.Version,
		Kind:                   gvk.Kind,
		ResourceVersion:        metaobj.GetResourceVersion(),
		ClusterResourceVersion: 0,
		Object:                 buffer.Bytes(),
		CreatedAt:              metaobj.GetCreationTimestamp().Time,
	}
	if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
		resource.DeletedAt = sql.NullTime{Time: deletedAt.Time, Valid: true}
	}

	result := s.db.WithContext(ctx).Create(&resource)
	return InterpretResourceDBError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) Update(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	var buffer bytes.Buffer
	if err := s.codec.Encode(obj, &buffer); err != nil {
		return err
	}

	var ownerUID types.UID
	if owner := metav1.GetControllerOfNoCopy(metaobj); owner != nil {
		ownerUID = owner.UID
	}

	// The uid may not be the same for resources with the same namespace/name
	// in the same cluster at different times.
	var updatedResource map[string]interface{}
	if crvUpdated {
		updatedResource = map[string]interface{}{
			"owner_uid":        ownerUID,
			"uid":              metaobj.GetUID(),
			"resource_version": metaobj.GetResourceVersion(),
			"object":           datatypes.JSON(buffer.Bytes()),
			"created_at":       metaobj.GetCreationTimestamp().Time,
			"published":        false,
			"deleted":          false,
		}
	} else {
		updatedResource = map[string]interface{}{
			"owner_uid":        ownerUID,
			"uid":              metaobj.GetUID(),
			"resource_version": metaobj.GetResourceVersion(),
			"object":           datatypes.JSON(buffer.Bytes()),
			"created_at":       metaobj.GetCreationTimestamp().Time,
		}
	}

	if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
		updatedResource["deleted_at"] = sql.NullTime{Time: deletedAt.Time, Valid: true}
	}

	result := s.db.WithContext(ctx).Model(&Resource{}).Where(map[string]interface{}{
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"namespace": metaobj.GetNamespace(),
		"name":      metaobj.GetName(),
	}).Updates(updatedResource)
	return InterpretResourceDBError(cluster, metaobj.GetName(), result.Error)
}

func (c *ResourceStorage) ConvertDeletedObject(obj interface{}) (runtime.Object, error) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return nil, err
	}

	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}

	// Since it is not necessary to save the complete deleted object to the queue,
	// we convert the object to `PartialObjectMetadata`
	return &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}, nil
}

func (s *ResourceStorage) deleteObject(cluster, namespace, name string) *gorm.DB {
	return s.db.Model(&Resource{}).Where(map[string]interface{}{
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"namespace": namespace,
		"name":      name,
	}).Delete(&Resource{})
}

func (s *ResourceStorage) Delete(ctx context.Context, cluster string, obj runtime.Object, crvUpdated bool) error {
	metaobj, err := meta.Accessor(obj)
	if err != nil {
		return err
	}

	// The uid may not be the same for resources with the same namespace/name
	// in the same cluster at different times.
	var updatedResource map[string]interface{}
	if crvUpdated {
		updatedResource = map[string]interface{}{
			"resource_version": metaobj.GetResourceVersion(),
			"deleted":          true,
			"published":        false,
		}
		if deletedAt := metaobj.GetDeletionTimestamp(); deletedAt != nil {
			updatedResource["deleted_at"] = sql.NullTime{Time: deletedAt.Time, Valid: true}
		}
	} else {
		updatedResource = map[string]interface{}{
			"deleted": true,
		}
	}

	condition := map[string]interface{}{
		"cluster":   cluster,
		"namespace": metaobj.GetNamespace(),
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
	}
	if metaobj.GetName() != "" {
		condition["name"] = metaobj.GetName()
	}

	result := s.db.WithContext(ctx).Model(&Resource{}).Where(condition).Updates(updatedResource)
	return InterpretResourceDBError(cluster, metaobj.GetName(), result.Error)
}

func (s *ResourceStorage) genGetObjectQuery(ctx context.Context, cluster, namespace, name string) *gorm.DB {
	condition := map[string]interface{}{
		"namespace": namespace,
		"name":      name,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"deleted":   false,
	}

	if cluster != "" {
		condition["cluster"] = cluster
	}
	return s.db.WithContext(ctx).Model(&Resource{}).Select("cluster_resource_version, object").Where(condition)
}

func (s *ResourceStorage) GetObj(ctx context.Context, cluster, namespace, name string) (runtime.Object, error) {
	var resource Resource
	condition := map[string]interface{}{
		"namespace": namespace,
		"name":      name,
		"cluster":   cluster,
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
	}

	result := s.db.WithContext(ctx).Model(&Resource{}).
		Select("cluster_resource_version, object").Where(condition).First(&resource)
	if result.Error != nil {
		return nil, InterpretResourceDBError(cluster, namespace+"/"+name, result.Error)
	}

	into := s.newFunc()
	obj, _, err := s.codec.Decode(resource.Object, nil, into)
	if err != nil {
		return nil, err
	}
	if obj != into {
		return nil, fmt.Errorf("Failed to decode resource, into is %T", into)
	}

	return obj, nil
}

func (s *ResourceStorage) Get(ctx context.Context, cluster, namespace, name string, into runtime.Object) error {
	var objects [][]byte
	if result := s.genGetObjectQuery(ctx, cluster, namespace, name).First(&objects); result.Error != nil {
		return InterpretResourceDBError(cluster, namespace+"/"+name, result.Error)
	}

	obj, _, err := s.codec.Decode(objects[0], nil, into)
	if err != nil {
		return err
	}
	if obj != into {
		return fmt.Errorf("Failed to decode resource, into is %T", into)
	}
	return nil
}

func (s *ResourceStorage) genListObjectsQuery(ctx context.Context, opts *internal.ListOptions, isAll bool) (int64, *int64, *gorm.DB, ObjectList, error) {
	var result ObjectList = &ResourceList{}
	if opts.OnlyMetadata {
		result = &ResourceMetadataList{}
	}

	var condition map[string]interface{}
	if !isAll {
		condition = map[string]interface{}{
			"group":    s.storageGroupResource.Group,
			"version":  s.storageVersion.Version,
			"resource": s.storageGroupResource.Resource,
			"deleted":  false,
		}
	}

	query := s.db.WithContext(ctx).Model(&Resource{}).Where(condition)
	offset, amount, query, err := applyListOptionsToResourceQuery(s.db, query, opts)
	return offset, amount, query, result, err
}

func (s *ResourceStorage) genListQuery(ctx context.Context, newfunc func() runtime.Object, opts *internal.ListOptions) ([]runtime.Object, error) {
	var result [][]byte

	condition := map[string]interface{}{
		"group":    s.storageGroupResource.Group,
		"version":  s.storageVersion.Version,
		"resource": s.storageGroupResource.Resource,
	}
	query := s.db.WithContext(ctx).Model(&Resource{}).Select("object").Where(condition)
	_, _, query, err := applyListOptionsToResourceQuery(s.db, query, opts)
	if err != nil {
		return nil, err
	}
	queryResult := query.Find(&result)
	if queryResult.Error != nil {
		return nil, queryResult.Error
	}

	length := len(result)
	objList := make([]runtime.Object, length)

	for index, value := range result {
		into := newfunc()
		obj, _, err := s.codec.Decode(value, nil, into)
		if err != nil {
			return nil, err
		}
		if obj != into {
			return nil, fmt.Errorf("Failed to decode resource, into is %T", into)
		}
		objList[index] = obj
	}

	return objList, nil
}

func (s *ResourceStorage) List(ctx context.Context, listObject runtime.Object, opts *internal.ListOptions) error {
	offset, amount, query, result, err := s.genListObjectsQuery(ctx, opts, true)
	if err != nil {
		return err
	}

	if err := result.From(query); err != nil {
		return InterpretDBError(s.storageGroupResource.String(), err)
	}
	objects := result.Items()

	list, err := meta.ListAccessor(listObject)
	if err != nil {
		return err
	}

	objects, crvs, maxCrv, err := getObjectListAndMaxCrv(objects, opts.OnlyMetadata)
	if err != nil {
		return err
	}
	if !utils.IsListOptsEmpty(opts) {
		maxCrv, err = s.GetMaxCrv(ctx)
		if err != nil {
			return err
		}
	}
	list.SetResourceVersion(maxCrv)

	if opts.WithContinue != nil && *opts.WithContinue {
		if int64(len(objects)) == opts.Limit {
			list.SetContinue(strconv.FormatInt(offset+opts.Limit, 10))
		}
	}

	if amount != nil {
		// When offset is too large, the data in the response is empty and the remaining count is negative.
		// This ensures that `amount = offset + len(objects) + remain`
		remain := *amount - offset - int64(len(objects))
		list.SetRemainingItemCount(&remain)
	}

	if len(objects) == 0 {
		return nil
	}

	accessor := meta.NewAccessor()

	if unstructuredList, ok := listObject.(*unstructured.UnstructuredList); ok {
		unstructuredList.Items = make([]unstructured.Unstructured, 0, len(objects))
		for i, object := range objects {
			uObj := &unstructured.Unstructured{}
			obj, err := object.ConvertTo(s.codec, uObj)
			if err != nil {
				return err
			}

			err = accessor.SetResourceVersion(obj, crvs[i])
			if err != nil {
				return fmt.Errorf("set resourceVersion failed: %v, unstructuredList", err)
			}

			uObj, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return genericstorage.NewInternalError("the converted object is not *unstructured.Unstructured")
			}

			if uObj.GroupVersionKind().Empty() {
				if version := unstructuredList.GetAPIVersion(); version != "" {
					// set to the same APIVersion as listObject
					uObj.SetAPIVersion(version)
				}
				if rt := object.GetResourceType(); !rt.Empty() {
					uObj.SetKind(rt.Kind)
				}
			}
			unstructuredList.Items = append(unstructuredList.Items, *uObj)
		}
		return nil
	}

	listPtr, err := meta.GetItemsPtr(listObject)
	if err != nil {
		return err
	}

	v, err := conversion.EnforcePtr(listPtr)
	if err != nil || v.Kind() != reflect.Slice {
		return fmt.Errorf("need ptr to slice: %v", err)
	}

	dedup := make(map[string]bool)
	expected := reflect.New(v.Type().Elem()).Interface().(runtime.Object)
	var dedupObjects []runtime.Object
	for _, object := range objects {
		obj, err := object.ConvertTo(s.codec, expected.DeepCopyObject())
		if err != nil {
			return err
		}

		resourceKey, err := s.KeyFunc(obj)
		if err != nil {
			return fmt.Errorf("keyfunc failed: %v, structedList", err)
		} else {
			if dedup[resourceKey] {
				continue
			}
			dedup[resourceKey] = true
			dedupObjects = append(dedupObjects, obj)
		}
	}

	slice := reflect.MakeSlice(v.Type(), len(dedupObjects), len(dedupObjects))
	for i, obj := range dedupObjects {
		err = accessor.SetResourceVersion(obj, crvs[i])
		if err != nil {
			return fmt.Errorf("set resourceVersion failed: %v, structedList", err)
		}
		slice.Index(i).Set(reflect.ValueOf(obj).Elem())
	}
	v.Set(slice)
	return nil
}

func (s *ResourceStorage) Watch(ctx context.Context, newfunc func() runtime.Object, options *internal.ListOptions, gvk schema.GroupVersionKind) (watch.Interface, error) {
	s.watchLock.Lock()
	defer s.watchLock.Unlock()
	initEvents, err := s.fetchInitEvents(ctx, options.ResourceVersion, newfunc, options)
	if err != nil {
		// To match the uncached watch implementation, once we have passed authn/authz/admission,
		// and successfully parsed a resource version, other errors must fail with a watch event of type ERROR,
		// rather than a directly returned error.
		return newErrWatcher(err), nil
	}

	watcher, err := watchcomponents.NewPredicateWatch(ctx, options, gvk, s.Namespaced)
	if err != nil {
		return newErrWatcher(err), nil
	}
	s.buffer.AppendWatcherBuffer(watcher)

	watcher.SetForget(func() {
		s.buffer.ForgetWatcher(watcher)
	})

	go watcher.Process(ctx, initEvents)
	return watcher, nil
}

func (s *ResourceStorage) ProcessEvent(ctx context.Context, eventType watch.EventType, obj runtime.Object, cluster string) error {
	newObj := obj.DeepCopyObject()
	event := watch.Event{
		Object: newObj,
		Type:   eventType,
	}
	s.eventChan <- &watchcomponents.EventWithCluster{
		Cluster: cluster,
		Event:   &event,
	}

	return nil
}

func (s *ResourceStorage) fetchInitEvents(ctx context.Context, rv string, newfunc func() runtime.Object, opts *internal.ListOptions) ([]*watch.Event, error) {
	if rv == "" {
		objects, err := s.genListQuery(ctx, newfunc, opts)
		if err != nil {
			return nil, err
		}

		result := make([]*watch.Event, len(objects))
		for index, value := range objects {
			event := &watch.Event{
				Object: value,
				Type:   watch.Added,
			}
			result[index] = event
		}
		return result, nil
	} else {
		result, err := s.eventCache.GetEvents(rv, func() (string, error) {
			return s.GetMaxCrv(ctx)
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}
}

func getObjectListAndMaxCrv(objList []Object, onlyMetada bool) ([]Object, []string, string, error) {
	crvs := make([]string, 0, len(objList))
	var maxCrv int64 = 0

	var objListNeed []Object
	if onlyMetada {
		for _, object := range objList {
			if metadata, ok := object.(ResourceMetadata); ok {
				if utils.IsBigger(metadata.ClusterResourceVersion, maxCrv) {
					maxCrv = metadata.ClusterResourceVersion
				}

				if metadata.Deleted {
					continue
				}
				objListNeed = append(objListNeed, object)
				crvs = append(crvs, utils.ParseInt642Str(metadata.ClusterResourceVersion))
			} else {
				return nil, nil, "0", fmt.Errorf("unknown object type")
			}
		}
		return objList, crvs, utils.ParseInt642Str(maxCrv), nil
	} else {
		for _, object := range objList {
			if resource, ok := object.(Resource); ok {
				if utils.IsBigger(resource.ClusterResourceVersion, maxCrv) {
					maxCrv = resource.ClusterResourceVersion
				}
				if resource.Deleted {
					continue
				}
				var b Bytes = []byte(resource.Object)
				crvs = append(crvs, utils.ParseInt642Str(resource.ClusterResourceVersion))
				objListNeed = append(objListNeed, b)
			} else {
				return nil, nil, "0", fmt.Errorf("unknown object type")
			}
		}
		return objListNeed, crvs, utils.ParseInt642Str(maxCrv), nil
	}
}

func (s *ResourceStorage) GetMaxCrv(ctx context.Context) (string, error) {
	maxCrv := "0"
	var metadataList ResourceMetadataList
	condition := map[string]interface{}{
		"group":    s.storageGroupResource.Group,
		"version":  s.storageVersion.Version,
		"resource": s.storageGroupResource.Resource,
	}
	result := s.db.WithContext(ctx).Model(&Resource{}).Select("cluster_resource_version").Where(condition).Order("cluster_resource_version DESC").Limit(1).Find(&metadataList)
	if result.Error != nil {
		return maxCrv, InterpretResourceDBError("", s.storageGroupResource.Resource, result.Error)
	}
	for _, metadata := range metadataList {
		maxCrv = utils.ParseInt642Str(metadata.ClusterResourceVersion)
	}
	return maxCrv, nil
}

// PublishEvent update column `ClusterResourceVersion` and `published` when event send to messagequeue middleware success.
func (s *ResourceStorage) PublishEvent(ctx context.Context, wc *watchcomponents.EventWithCluster) {
	metaObj, err := meta.Accessor(wc.Event.Object)
	if err != nil {
		return
	}

	crv, err := utils.ParseStr2Int64(metaObj.GetResourceVersion())
	if err != nil {
		klog.Errorf("Crv failed to convert int64, name: %s, namespace: %s, cluster: %s, err: %v",
			metaObj.GetName(), metaObj.GetNamespace(), wc.Cluster, err)
	}
	// The uid may not be the same for resources with the same namespace/name
	// in the same cluster at different times.
	updatedResource := map[string]interface{}{
		"ClusterResourceVersion": crv,
		"published":              true,
	}

	condition := map[string]interface{}{
		"group":     s.storageGroupResource.Group,
		"version":   s.storageVersion.Version,
		"resource":  s.storageGroupResource.Resource,
		"cluster":   wc.Cluster,
		"namespace": metaObj.GetNamespace(),
		"name":      metaObj.GetName(),
	}

	s.db.WithContext(ctx).Model(&Resource{}).Where(condition).Updates(updatedResource)
}

func (s *ResourceStorage) GenCrv2Event(event *watch.Event) {
	accessor := meta.NewAccessor()
	err := accessor.SetResourceVersion(event.Object, utils.ParseInt642Str(time.Now().UnixMicro()))
	if err != nil {
		klog.Errorf("set resourceVersion failed: %v, may be it's a clear event", err)
	}
}

func applyListOptionsToResourceQuery(db *gorm.DB, query *gorm.DB, opts *internal.ListOptions) (int64, *int64, *gorm.DB, error) {
	applyFn := func(query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error) {
		query, err := applyOwnerToResourceQuery(db, query, opts)
		if err != nil {
			return nil, err
		}

		return query, nil
	}

	return applyListOptionsToQuery(query, opts, applyFn)
}

func applyOwnerToResourceQuery(db *gorm.DB, query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error) {
	var ownerQuery interface{}
	switch {
	case len(opts.ClusterNames) != 1:
		return query, nil

	case opts.OwnerUID != "":
		ownerQuery = buildOwnerQueryByUID(db, opts.ClusterNames[0], opts.OwnerUID, opts.OwnerSeniority)

	case opts.OwnerName != "":
		var ownerNamespaces []string
		if len(opts.Namespaces) != 0 {
			// match namespaced and clustered owner resources
			ownerNamespaces = append(opts.Namespaces, "")
		}
		ownerQuery = buildOwnerQueryByName(db, opts.ClusterNames[0], ownerNamespaces, opts.OwnerGroupResource, opts.OwnerName, opts.OwnerSeniority)

	default:
		return query, nil
	}

	if _, ok := ownerQuery.(string); ok {
		query = query.Where("owner_uid = ?", ownerQuery)
	} else {
		query = query.Where("owner_uid IN (?)", ownerQuery)
	}
	return query, nil
}

type errWatcher struct {
	result chan watch.Event
}

func newErrWatcher(err error) *errWatcher {
	errEvent := watchutil.NewErrorEvent(err)

	// Create a watcher with room for a single event, populate it, and close the channel
	watcher := &errWatcher{result: make(chan watch.Event, 1)}
	watcher.result <- errEvent
	close(watcher.result)

	return watcher
}

func (c *errWatcher) ResultChan() <-chan watch.Event {
	return c.result
}

func (c *errWatcher) Stop() {
	// no-op
}
