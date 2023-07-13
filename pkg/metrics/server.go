package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

type Config struct {
	Endpoint string

	TLSConfig           string
	DisableGZIPEncoding bool
}

func RunServer(config Config) {
	handlers := []Handler{
		{
			LandingName: "Metrics",
			Path:        "metrics",
			Handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{
				ErrorLog:           Logger,
				DisableCompression: config.DisableGZIPEncoding,
			}),
		},
	}
	server, flags := BuildMetricsServer(
		config.Endpoint,
		config.TLSConfig,
		"clusterpedia clustersynchro manager",
		"Self-metrics for clusterpedia clustersynchro manager",
		handlers,
	)

	klog.Info("Metrics Server is running...")
	web.ListenAndServe(server, flags, Logger)
}

type Handler struct {
	LandingName string
	Path        string
	Handler     http.Handler
}

func BuildMetricsServer(endpoint, tlsConfigFile, name, desc string, handlers []Handler) (*http.Server, *web.FlagConfig) {
	var links []web.LandingLinks
	mux := http.NewServeMux()
	for _, handler := range handlers {
		mux.Handle(handler.Path, handler.Handler)
		if handler.LandingName != "" {
			links = append(links, web.LandingLinks{
				Address: handler.Path,
				Text:    handler.LandingName,
			})
		}
	}

	// Add index
	landingConfig := web.LandingConfig{
		Name:        name,
		Description: desc,
		Version:     version.Get().String(),
		Links:       links,
	}
	landingPage, err := web.NewLandingPage(landingConfig)
	if err != nil {
		klog.ErrorS(err, "failed to create landing page")
	}
	mux.Handle("/", landingPage)

	return &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		&web.FlagConfig{
			WebListenAddresses: &[]string{endpoint},
			WebSystemdSocket:   new(bool),
			WebConfigFile:      &tlsConfigFile,
		}
}
