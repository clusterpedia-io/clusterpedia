package scheme

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia/install"
)

var scheme = runtime.NewScheme()

var Codecs = serializer.NewCodecFactory(scheme)

var ParameterCodec = runtime.NewParameterCodec(scheme)

func init() {
	install.Install(scheme)
}
