package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
)

const (

	// ResourcePathWithoutClusterpediaPrefix is a feature gate for rewrite apiserver request's URL
	// owner: @huiwq1990
	// alpha: v0.8.0
	ResourcePathWithoutClusterpediaPrefix featuregate.Feature = "ResourcePathWithoutClusterpediaPrefix"
)

func init() {
	runtime.Must(clusterpediafeature.MutableFeatureGate.Add(defaultResourcePathWithoutClusterpediaPrefixFeatureGates))
}

// defaultResourcePathWithoutClusterpediaPrefixFeatureGates consists of all known apiserver feature keys.
// To add a new feature, define a key for it above and add it here.
var defaultResourcePathWithoutClusterpediaPrefixFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ResourcePathWithoutClusterpediaPrefix: {Default: false, PreRelease: featuregate.Alpha},
}
