package kubestatemetrics

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/prometheus/common/expfmt"
	"k8s.io/klog/v2"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"
)

type ClusterMetricsWriterListGetter interface {
	GetMetricsWriterList() map[string]metricsstore.MetricsWriterList
}

// MetricsHandler is a http.Handler that exposes the main kube-state-metrics
// /metrics endpoint. It allows concurrent reconfiguration at runtime.
type MetricsHandler struct {
	disableGZIPEncoding bool
	getter              ClusterMetricsWriterListGetter
}

// New creates and returns a new MetricsHandler with the given options.
func NewMetricsHandler(getter ClusterMetricsWriterListGetter, diableGZIPEncoding bool) *MetricsHandler {
	return &MetricsHandler{
		getter:              getter,
		disableGZIPEncoding: diableGZIPEncoding,
	}
}

// ServeHTTP implements the http.Handler interface. It writes all generated
// metrics to the response body.
func (m *MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var writers metricsstore.MetricsWriterList

	clusterWriters := m.getter.GetMetricsWriterList()
	if cluster := mux.Vars(r)["cluster"]; cluster != "" {
		writers = clusterWriters[cluster]
	} else {
		for _, wl := range clusterWriters {
			writers = append(writers, wl...)
		}
	}

	resHeader := w.Header()
	var writer io.Writer = w

	contentType := expfmt.NegotiateIncludingOpenMetrics(r.Header)

	// We do not support protobuf at the moment. Fall back to FmtText if the negotiated exposition format is not FmtOpenMetrics See: https://github.com/kubernetes/kube-state-metrics/issues/2022
	if contentType != expfmt.FmtOpenMetrics_1_0_0 && contentType != expfmt.FmtOpenMetrics_0_0_1 {
		contentType = expfmt.FmtText
	}
	resHeader.Set("Content-Type", string(contentType))

	if !m.disableGZIPEncoding {
		// Gzip response if requested. Taken from
		// github.com/prometheus/client_golang/prometheus/promhttp.decorateWriter.
		reqHeader := r.Header.Get("Accept-Encoding")
		parts := strings.Split(reqHeader, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "gzip" || strings.HasPrefix(part, "gzip;") {
				writer = gzip.NewWriter(writer)
				resHeader.Set("Content-Encoding", "gzip")
			}
		}
	}

	metricsWriters := metricsstore.SanitizeHeaders(writers)
	for _, w := range metricsWriters {
		err := w.WriteAll(writer)
		if err != nil {
			klog.ErrorS(err, "Failed to write metrics")
		}
	}

	// OpenMetrics spec requires that we end with an EOF directive.
	if contentType == expfmt.FmtOpenMetrics_1_0_0 || contentType == expfmt.FmtOpenMetrics_0_0_1 {
		_, err := writer.Write([]byte("# EOF\n"))
		if err != nil {
			klog.ErrorS(err, "Failed to write EOF directive")
		}
	}

	// In case we gzipped the response, we have to close the writer.
	if closer, ok := writer.(io.Closer); ok {
		err := closer.Close()
		if err != nil {
			klog.ErrorS(err, "Failed to close the writer")
		}
	}
}
