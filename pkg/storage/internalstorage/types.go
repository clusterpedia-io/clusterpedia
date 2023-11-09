package internalstorage

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
)

type Object interface {
	GetResourceType() ResourceType
	ConvertToUnstructured() (*unstructured.Unstructured, error)
	ConvertTo(codec runtime.Codec, object runtime.Object) (runtime.Object, error)
}

type ObjectList interface {
	From(db *gorm.DB) error
	Items() []Object
}

type ResourceType struct {
	Group    string
	Version  string
	Resource string
	Kind     string
}

func (rt ResourceType) Empty() bool {
	return rt == ResourceType{}
}

func (rt ResourceType) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    rt.Group,
		Version:  rt.Version,
		Resource: rt.Resource,
	}
}

type Resource struct {
	ID uint `gorm:"primaryKey"`

	Group    string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Version  string `gorm:"size:15;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Resource string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Kind     string `gorm:"size:63;not null"`

	Cluster                string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100;index:idx_cluster"`
	Namespace              string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:50;index:idx_group_version_resource_namespace_name"`
	Name                   string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	OwnerUID               types.UID `gorm:"column:owner_uid;size:36;not null;default:''"`
	UID                    types.UID `gorm:"size:36;not null"`
	ResourceVersion        string    `gorm:"size:30;not null"`
	ClusterResourceVersion int64

	Object datatypes.JSON `gorm:"not null"`

	CreatedAt time.Time `gorm:"not null"`
	SyncedAt  time.Time `gorm:"not null;autoUpdateTime"`
	DeletedAt sql.NullTime
}

func (res Resource) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    res.Group,
		Version:  res.Version,
		Resource: res.Resource,
	}
}

func (res Resource) GetResourceType() ResourceType {
	return ResourceType{
		Group:    res.Group,
		Version:  res.Version,
		Resource: res.Resource,
		Kind:     res.Kind,
	}
}

func (res Resource) ConvertToUnstructured() (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(res.Object, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (res Resource) ConvertTo(codec runtime.Codec, object runtime.Object) (runtime.Object, error) {
	obj, _, err := codec.Decode(res.Object, nil, object)
	return obj, err
}

type ResourceMetadata struct {
	ResourceType           `gorm:"embedded"`
	ClusterResourceVersion int64
	Metadata               datatypes.JSON
}

func (data ResourceMetadata) ConvertToUnstructured() (*unstructured.Unstructured, error) {
	metadata := map[string]interface{}{}
	if err := json.Unmarshal(data.Metadata, &metadata); err != nil {
		return nil, err
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": schema.GroupVersion{Group: data.Group, Version: data.Version}.String(),
			"kind":       data.Kind,
			"metadata":   metadata,
		},
	}, nil
}

func (data ResourceMetadata) ConvertTo(codec runtime.Codec, object runtime.Object) (runtime.Object, error) {
	if uObj, ok := object.(*unstructured.Unstructured); ok {
		if uObj.Object == nil {
			uObj.Object = make(map[string]interface{}, 1)
		}

		metadata := map[string]interface{}{}
		if err := json.Unmarshal(data.Metadata, &metadata); err != nil {
			return nil, err
		}

		// There may be version conversions in the codec,
		// so we cannot use data.ResourceType to override `APIVersion` and `Kind`.
		uObj.Object["metadata"] = metadata
		return uObj, nil
	}

	metadata := metav1.ObjectMeta{}
	if err := json.Unmarshal(data.Metadata, &metadata); err != nil {
		return nil, err
	}
	v := reflect.ValueOf(object)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		return nil, errors.New("object is nil or not pointer")
	}

	// There may be version conversions in the codec,
	// so we cannot use data.ResourceType to override `APIVersion` and `Kind`.
	v.Elem().FieldByName("ObjectMeta").Set(reflect.ValueOf(metadata))
	return object, nil
}

func (data ResourceMetadata) GetResourceType() ResourceType {
	return data.ResourceType
}

type Bytes datatypes.JSON

func (bytes *Bytes) Scan(data any) error {
	return (*datatypes.JSON)(bytes).Scan(data)
}

func (bytes Bytes) Value() (driver.Value, error) {
	return (datatypes.JSON)(bytes).Value()
}

func (bytes Bytes) ConvertToUnstructured() (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	if err := json.Unmarshal(bytes, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (bytes Bytes) ConvertTo(codec runtime.Codec, object runtime.Object) (runtime.Object, error) {
	obj, _, err := codec.Decode(bytes, nil, object)
	return obj, err
}

func (bytes Bytes) GetResourceType() ResourceType {
	return ResourceType{}
}

type ResourceList []Resource

func (list *ResourceList) From(db *gorm.DB) error {
	resources := []Resource{}
	if result := db.Find(&resources); result.Error != nil {
		return result.Error
	}
	*list = resources
	return nil
}

func (list ResourceList) Items() []Object {
	objects := make([]Object, 0, len(list))
	for _, object := range list {
		objects = append(objects, object)
	}
	return objects
}

type ResourceMetadataList []ResourceMetadata

func (list *ResourceMetadataList) From(db *gorm.DB) error {
	switch db.Dialector.Name() {
	case "sqlite", "sqlite3", "mysql":
		db = db.Select("`group`, version, resource, kind, object->>'$.metadata' as metadata")
	case "postgres":
		db = db.Select(`"group", version, resource, kind, object->>'metadata' as metadata`)
	default:
		panic("storage: only support sqlite3, mysql or postgres")
	}
	metadatas := []ResourceMetadata{}
	if result := db.Find(&metadatas); result.Error != nil {
		return result.Error
	}
	*list = metadatas
	return nil
}

func (list ResourceMetadataList) Items() []Object {
	objects := make([]Object, 0, len(list))
	for _, object := range list {
		objects = append(objects, object)
	}
	return objects
}

type BytesList []Bytes

func (list *BytesList) From(db *gorm.DB) error {
	if result := db.Select("object").Find(list); result.Error != nil {
		return result.Error
	}
	return nil
}

func (list BytesList) Items() []Object {
	objects := make([]Object, 0, len(list))
	for _, object := range list {
		objects = append(objects, object)
	}
	return objects
}
