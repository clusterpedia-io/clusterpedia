package metrics

import (
	versionCollector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"k8s.io/component-base/metrics/legacyregistry"
)

func init() {
	legacyregistry.RawMustRegister(
		versionCollector.NewCollector("clusterpedia_kube_state_metrics"),
	)
}
