package feature

import (
	"k8s.io/component-base/featuregate"
)

var (
	// MutableFeatureGate is a mutable version of FeatureGate.
	// Only top-level commands/options setup and the k8s.io/component-base/featuregate/testing package should make use of this.
	MutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// FeatureGate is a shared global FeatureGate.
	// Top-level commands/options setup that needs to modify this featuregate gate should use MutableFeatureGate.
	FeatureGate featuregate.FeatureGate = MutableFeatureGate
)
