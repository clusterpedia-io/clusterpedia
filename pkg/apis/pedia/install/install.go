package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	"github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia/v1alpha1"
)

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(pedia.Install(scheme))
	utilruntime.Must(v1alpha1.Install(scheme))
	utilruntime.Must(scheme.SetVersionPriority(v1alpha1.SchemeGroupVersion))
}
