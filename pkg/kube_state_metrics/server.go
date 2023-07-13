package kubestatemetrics

import (
	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"k8s.io/klog/v2"
)

type ServerConfig struct {
	Endpoint            string
	TLSConfig           string
	DisableGZIPEncoding bool
}

func RunServer(config ServerConfig, getter ClusterMetricsWriterListGetter) {
	durationVec := promauto.With(metrics.DefaultRegistry()).NewHistogramVec(
		prometheus.HistogramOpts{
			Name:        "http_request_duration_seconds",
			Help:        "A histogram of requests for clusterpedia's kube-state-metrics metrics handler.",
			Buckets:     prometheus.DefBuckets,
			ConstLabels: prometheus.Labels{"handler": "metrics"},
		}, []string{"method"},
	)

	handlers := []metrics.Handler{
		{
			LandingName: "Metrics",
			Path:        "/metrics",
			Handler: promhttp.InstrumentHandlerDuration(durationVec,
				NewMetricsHandler(getter, config.DisableGZIPEncoding),
			),
		},
	}
	server, flags := metrics.BuildMetricsServer(
		config.Endpoint,
		config.TLSConfig,
		"Clusterpedia kube-state-metrics",
		"Metrics for Kubernetes' state managed by Clusterpedia",
		handlers,
	)

	klog.Info("Kube State Metrics Server is running...")
	// TODO(iceber): handle error
	web.ListenAndServe(server, flags, metrics.Logger)
}
