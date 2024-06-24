package resourceconfig

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type ResourceConfig struct {
	Namespaced    bool
	GroupResource schema.GroupResource

	StorageResource schema.GroupVersionResource
	MemoryResource  schema.GroupVersionResource

	Codec runtime.Codec
}
