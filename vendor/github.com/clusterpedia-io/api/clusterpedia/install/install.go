package install

import (
	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/api/clusterpedia/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(internal.Install(scheme))
	utilruntime.Must(v1beta1.Install(scheme))
	utilruntime.Must(scheme.SetVersionPriority(v1beta1.SchemeGroupVersion))
}
