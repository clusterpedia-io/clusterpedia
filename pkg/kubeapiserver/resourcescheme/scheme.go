package resourcescheme

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubernetes/pkg/api/legacyscheme"

	unstructuredresourcescheme "github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcescheme/unstructured"
)

var (
	LegacyResourceScheme         = legacyscheme.Scheme
	LegacyResourceCodecs         = legacyscheme.Codecs
	LegacyResourceParameterCodec = legacyscheme.ParameterCodec

	UnstructuredScheme = unstructuredresourcescheme.NewScheme()
	UnstructuredCodecs = unstructured.UnstructuredJSONScheme
)
