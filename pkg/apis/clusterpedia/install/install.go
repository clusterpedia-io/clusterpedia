package install

import (
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	internal "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia/v1beta1"
)

func Install(scheme *runtime.Scheme) {
	utilruntime.Must(internal.Install(scheme))
	utilruntime.Must(v1beta1.Install(scheme))
	utilruntime.Must(scheme.SetVersionPriority(v1beta1.SchemeGroupVersion))
}
