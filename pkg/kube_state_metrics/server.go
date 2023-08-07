package kubestatemetrics

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"text/template"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/version"
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

	server := &http.Server{
		Handler:           buildMetricsServer(config, getter, durationVec),
		ReadHeaderTimeout: 5 * time.Second,
	}

	flags := &web.FlagConfig{
		WebListenAddresses: &[]string{config.Endpoint},
		WebSystemdSocket:   new(bool),
		WebConfigFile:      &config.TLSConfig,
	}

	klog.Info("Kube State Metrics Server is running...")
	// TODO(iceber): handle error
	_ = web.ListenAndServe(server, flags, metrics.Logger)
}

func buildMetricsServer(config ServerConfig, getter ClusterMetricsWriterListGetter, durationObserver prometheus.ObserverVec) *mux.Router {
	handler := promhttp.InstrumentHandlerDuration(durationObserver,
		NewMetricsHandler(getter, config.DisableGZIPEncoding),
	)
	mux := mux.NewRouter()
	mux.Handle("/metrics", handler)
	mux.Handle("/clusters/{cluster}/metrics", handler)

	// Add index
	landingConfig := web.LandingConfig{
		Name:        "Clusterpedia kube-state-metrics",
		Description: "Metrics for Kubernetes' state managed by Clusterpedia",
		Version:     version.Get().String(),
		Links: []web.LandingLinks{
			{
				Text:    "Metrics",
				Address: "/metrics",
			},
		},
	}
	landingPage, err := NewLandingPage(landingConfig, getter)
	if err != nil {
		klog.ErrorS(err, "failed to create landing page")
	}
	mux.Handle("/", landingPage)
	return mux
}

var (
	//go:embed landing_page/landing_page.html
	landingPagehtmlContent string
	//go:embed landing_page/landing_page.css
	landingPagecssContent string
)

func NewLandingPage(c web.LandingConfig, getter ClusterMetricsWriterListGetter) (*LandingPageHandler, error) {
	length := 0
	for _, input := range c.Form.Inputs {
		inputLength := len(input.Label)
		if inputLength > length {
			length = inputLength
		}
	}
	c.Form.Width = (float64(length) + 1) / 2
	if c.CSS == "" {
		if c.HeaderColor == "" {
			// Default to Prometheus orange.
			c.HeaderColor = "#e6522c"
		}
		cssTemplate := template.Must(template.New("landing css").Parse(landingPagecssContent))
		var buf bytes.Buffer
		if err := cssTemplate.Execute(&buf, c); err != nil {
			return nil, err
		}
		c.CSS = buf.String()
	}
	return &LandingPageHandler{
		config:   c,
		template: template.Must(template.New("landing page").Parse(landingPagehtmlContent)),
		getter:   getter,
	}, nil
}

type LandingPageHandler struct {
	config   web.LandingConfig
	template *template.Template

	getter ClusterMetricsWriterListGetter
}

func (h *LandingPageHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	clusters := h.getter.GetMetricsWriterList()

	config := h.config
	config.Links = append(make([]web.LandingLinks, 0, len(config.Links)+len(clusters)),
		h.config.Links...,
	)
	for cluster := range clusters {
		config.Links = append(config.Links, web.LandingLinks{
			Text:    cluster + " Metrics",
			Address: fmt.Sprintf("/clusters/%s/metrics", cluster),
		})
	}

	var buf bytes.Buffer
	if err := h.template.Execute(&buf, config); err != nil {
		_, _ = w.Write([]byte(err.Error()))
		return
	}

	_, _ = w.Write(buf.Bytes())
	w.Header().Add("Content-Type", "text/html; charset=UTF-8")
}
