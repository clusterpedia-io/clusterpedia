package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add method here following this template:
	//
	// // owner: @username
	// // alpha: v0.X
	// MyFeature featuregate.Feature = "MyFeature".

	// PruneManagedFields is a feature gate for ClusterSynchro to prune `ManagedFields` of the resource
	// https://kubernetes.io/docs/reference/using-api/server-side-apply/
	//
	// owner: @iceber
	// alpha: v0.0.9
	// beta: v0.3.0
	PruneManagedFields featuregate.Feature = "PruneManagedFields"

	// PruneLastAppliedConfiguration is a feature gate for the ClusterSynchro to prune `LastAppliedConfiguration` of the resource
	//
	// owner: @iceber
	// alpha: v0.0.9
	// beta: v0.3.0
	PruneLastAppliedConfiguration featuregate.Feature = "PruneLastAppliedConfiguration"

	// AllowSyncAllCustomResources is a feature gate for the ClusterSynchro to allow syncing of all custom resources
	//
	// owner: @iceber
	// alpha: v0.3.0
	AllowSyncAllCustomResources featuregate.Feature = "AllowSyncAllCustomResources"

	// AllowSyncAllResources is a feature gate for the ClusterSynchro to allow syncing of all resources
	//
	// owner: @iceber
	// alpha: v0.3.0
	AllowSyncAllResources featuregate.Feature = "AllowSyncAllResources"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultClusterSynchroManagerFeatureGates))
}

// defaultClusterSynchroManagerFeatureGates consists of all known clustersynchro-manager-specific feature keys.
// To add a new feature, define a key for it above and add it here.
var defaultClusterSynchroManagerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	PruneManagedFields:            {Default: true, PreRelease: featuregate.Beta},
	PruneLastAppliedConfiguration: {Default: true, PreRelease: featuregate.Beta},
	AllowSyncAllCustomResources:   {Default: false, PreRelease: featuregate.Alpha},
	AllowSyncAllResources:         {Default: false, PreRelease: featuregate.Alpha},
}
