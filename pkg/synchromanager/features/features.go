package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
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

	// HealthCheckerWithStandaloneTCP is a feature gate for the cluster health checker to use standalone tcp
	//
	// owner: @iceber
	// alpha: v0.6.0
	HealthCheckerWithStandaloneTCP featuregate.Feature = "HealthCheckerWithStandaloneTCP"

	// ForcePaginatedListForResourceSync is a feature gate for ResourceSync's reflector to force paginated list,
	// reflector will sometimes use APIServer's cache, even if paging is specified APIServer will return all resources for performance,
	// then it will skip Reflector's streaming memory optimization.
	//
	// owner: @iceber
	// alpha: v0.8.0
	ForcePaginatedListForResourceSync featuregate.Feature = "ForcePaginatedListForResourceSync"

	// StreamHandlePaginatedListForResourceSync is a feature gate for ResourceSync's reflector to handle echo paginated resources,
	// resources within a pager will be processed as soon as possible instead of waiting until all resources are pulled before calling the ResourceHandler.
	//
	// owner: @iceber
	// alpha: v0.8.0
	StreamHandlePaginatedListForResourceSync featuregate.Feature = "StreamHandlePaginatedListForResourceSync"

	// IgnoreSyncLease is a feature gate for the ClusterSynchro to skip syncing leases.coordination.k8s.io,
	// if you enable this feature, these resources will not be synced no matter what `syncResources` are defined.
	//
	// owner: @27149chen
	// alpha: v0.8.0
	IgnoreSyncLease featuregate.Feature = "IgnoreSyncLease"
)

func init() {
	runtime.Must(clusterpediafeature.MutableFeatureGate.Add(defaultClusterSynchroManagerFeatureGates))
}

// defaultClusterSynchroManagerFeatureGates consists of all known clustersynchro-manager-specific feature keys.
// To add a new feature, define a key for it above and add it here.
var defaultClusterSynchroManagerFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	PruneManagedFields:             {Default: true, PreRelease: featuregate.Beta},
	PruneLastAppliedConfiguration:  {Default: true, PreRelease: featuregate.Beta},
	AllowSyncAllCustomResources:    {Default: false, PreRelease: featuregate.Alpha},
	AllowSyncAllResources:          {Default: false, PreRelease: featuregate.Alpha},
	HealthCheckerWithStandaloneTCP: {Default: false, PreRelease: featuregate.Alpha},

	ForcePaginatedListForResourceSync:        {Default: false, PreRelease: featuregate.Alpha},
	StreamHandlePaginatedListForResourceSync: {Default: false, PreRelease: featuregate.Alpha},
	IgnoreSyncLease:                          {Default: false, PreRelease: featuregate.Alpha},
}
