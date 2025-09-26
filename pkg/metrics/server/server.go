package server

import (
	"net/http"
	"time"

	"github.com/prometheus/exporter-toolkit/web"
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/klog/v2"

	_ "github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/pprof"
	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

type Config struct {
	Endpoint string

	TLSConfig           string
	DisableGZIPEncoding bool
}

func Run(config Config) {
	server := &http.Server{
		Handler:           buildMetricsServer(config),
		ReadHeaderTimeout: 6 * time.Second,
	}

	flags := &web.FlagConfig{
		WebListenAddresses: &[]string{config.Endpoint},
		WebSystemdSocket:   new(bool),
		WebConfigFile:      &config.TLSConfig,
	}

	klog.Info("Metrics Server is running...")
	if err := web.ListenAndServe(server, flags, Logger); err != nil {
		klog.ErrorS(err, "Metrics Server error")
	}
}

func buildMetricsServer(config Config) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.HandlerWithReset(
		legacyregistry.DefaultGatherer.(metrics.KubeRegistry),
		metrics.HandlerOpts{
			ErrorLog:           Logger,
			DisableCompression: config.DisableGZIPEncoding,
		}),
	)
	// add profiler
	pprof.RegisterProfileHandler(mux)
	// Add index
	landingConfig := web.LandingConfig{
		Name:        "clusterpedia clustersynchro manager",
		Description: "Self-metrics for clusterpedia clustersynchro manager",
		Version:     version.Get().String(),
		Links: []web.LandingLinks{
			{
				Text:    "Metrics",
				Address: "/metrics",
			},
		},
	}
	landingPage, err := web.NewLandingPage(landingConfig)
	if err != nil {
		klog.ErrorS(err, "failed to create landing page")
	}
	mux.Handle("/", landingPage)
	return mux
}
