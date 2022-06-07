package internalstorage

import (
	"database/sql"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
)

type Resource struct {
	ID uint `gorm:"primaryKey"`

	Group    string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Version  string `gorm:"size:15;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Resource string `gorm:"size:63;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	Kind     string `gorm:"size:63;not null"`

	Cluster         string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100;index:idx_cluster"`
	Namespace       string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:50;index:idx_group_version_resource_namespace_name"`
	Name            string    `gorm:"size:253;not null;uniqueIndex:uni_group_version_resource_cluster_namespace_name,length:100;index:idx_group_version_resource_namespace_name;index:idx_group_version_resource_name"`
	OwnerUID        types.UID `gorm:"column:owner_uid;size:36;not null;default:''"`
	UID             types.UID `gorm:"size:36;not null"`
	ResourceVersion string    `gorm:"size:30;not null"`

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

type Object interface {
	GetResourceType() ResourceType
	ConvertToUnstructured() (*unstructured.Unstructured, error)
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

func (res ResourceType) GroupVersionResource() schema.GroupVersionResource {
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

type ResourceMetadata struct {
	ResourceType `gorm:"embedded"`

	Metadata datatypes.JSONMap
}

func (data ResourceMetadata) ConvertToUnstructured() (*unstructured.Unstructured, error) {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": schema.GroupVersion{Group: data.Group, Version: data.Version}.String(),
			"kind":       data.Kind,
			"metadata":   map[string]interface{}(data.Metadata),
		},
	}, nil
}

func (data ResourceMetadata) GetResourceType() ResourceType {
	return data.ResourceType
}

type ResourceMetadataList []ResourceMetadata

func (list *ResourceMetadataList) From(db *gorm.DB) error {
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
