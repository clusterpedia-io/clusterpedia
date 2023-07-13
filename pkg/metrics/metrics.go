package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	prometheusversion "github.com/prometheus/common/version"
)

var registry = prometheus.NewRegistry()

func DefaultRegistry() prometheus.Registerer {
	return registry
}

func init() {
	registry.MustRegister(prometheusversion.NewCollector("clusterpedia_kube_state_metrics"))
}
