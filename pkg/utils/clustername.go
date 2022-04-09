package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

func ExtractClusterName(obj runtime.Object) string {
	if m, err := meta.Accessor(obj); err == nil {
		if annotations := m.GetAnnotations(); annotations != nil {
			return annotations[internal.ShadowAnnotationClusterName]
		}
	}
	return ""
}

func InjectClusterName(obj runtime.Object, name string) {
	m, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	annotations := m.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}

	annotations[internal.ShadowAnnotationClusterName] = name
	m.SetAnnotations(annotations)
}
