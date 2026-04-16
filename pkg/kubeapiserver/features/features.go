package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// AllowProxyRequestToClusters is a feature gate for the apiserver to handle proxy and forward requests.
	//
	// owner: @scydas
	// alpha: v0.9.0
	AllowProxyRequestToClusters featuregate.Feature = "AllowProxyRequestToClusters"

	// ClusterAuthenticationFromSecret could get authentication information of the PediaCluster from Secret.
	//
	// owner: @scydas
	// alpha: v0.9.0
	ClusterAuthenticationFromSecret featuregate.Feature = "ClusterAuthenticationFromSecret"

	// NotConvertToMemoryVersion could instaed of converting resources to memory version in request handling,
	// it is permitted to use the storage version in some cases.

	// owner: @Iceber
	// alpha: v0.9.0
	NotConvertToMemoryVersion featuregate.Feature = "NotConvertToMemoryVersion"

	// RemainingItemCount is a feature gate for the apiserver to include the remaining item count
	// in list responses. This was previously an upstream Kubernetes feature gate that graduated to GA
	// and was removed in Kubernetes 1.35. Clusterpedia re-registers it independently so that the
	// behavior can still be controlled.
	// https://github.com/clusterpedia-io/clusterpedia/issues/196
	RemainingItemCount featuregate.Feature = "RemainingItemCount"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultInternalStorageFeatureGates))
}

// defaultInternalStorageFeatureGates consists of all known custom internalstorage feature keys.
// To add a new feature, define a key for it above and add it here.
var defaultInternalStorageFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	AllowProxyRequestToClusters:     {Default: false, PreRelease: featuregate.Alpha},
	ClusterAuthenticationFromSecret: {Default: false, PreRelease: featuregate.Alpha},
	NotConvertToMemoryVersion:       {Default: false, PreRelease: featuregate.Alpha},
	RemainingItemCount:              {Default: false, PreRelease: featuregate.Alpha},
}
