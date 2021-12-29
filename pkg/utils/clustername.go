package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	pedia "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
)

func ExtractClusterName(obj runtime.Object) string {
	if m, err := meta.Accessor(obj); err == nil {
		if annotations := m.GetAnnotations(); annotations != nil {
			return annotations[pedia.ShadowAnnotationClusterName]
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

	annotations[pedia.ShadowAnnotationClusterName] = name
	m.SetAnnotations(annotations)
}
