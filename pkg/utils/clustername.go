package utils

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	pedia "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
)

func ExtractClusterName(obj runtime.Object) string {
	if m, err := meta.Accessor(obj); err == nil {
		if labels := m.GetLabels(); labels != nil {
			return labels[pedia.ShadowLabelClusterName]
		}
	}
	return ""
}

func InjectClusterName(obj runtime.Object, name string) {
	m, err := meta.Accessor(obj)
	if err != nil {
		panic(err)
	}

	labels := m.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}

	labels[pedia.ShadowLabelClusterName] = name
	m.SetLabels(labels)
}
