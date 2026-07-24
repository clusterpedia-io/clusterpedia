package metrics

import (
	"strconv"
	"time"

	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const apiserverSubsystem = "clusterpedia_apiserver"

var (
	requestTotal = compbasemetrics.NewCounterVec(
		&compbasemetrics.CounterOpts{
			Subsystem: apiserverSubsystem,
			Name:      "request_total",
			Help:      "Counter of HTTP requests processed by Clusterpedia APIServer, broken out by status code, handler and method.",
		},
		[]string{"code", "handler", "method"},
	)

	requestDuration = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Subsystem: apiserverSubsystem,
			Name:      "request_duration_seconds",
			Help:      "Response latency distribution in seconds for Clusterpedia APIServer, broken out by status code, handler and method.",
			Buckets: []float64{0.005, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		},
		[]string{"code", "handler", "method"},
	)

	responseSize = compbasemetrics.NewHistogramVec(
		&compbasemetrics.HistogramOpts{
			Subsystem: apiserverSubsystem,
			Name:      "response_size_bytes",
			Help:      "Response size distribution in bytes for Clusterpedia APIServer, broken out by status code, handler and method.",
			Buckets:   compbasemetrics.ExponentialBuckets(100, 10, 8),
		},
		[]string{"code", "handler", "method"},
	)

	requestInflight = compbasemetrics.NewGaugeVec(
		&compbasemetrics.GaugeOpts{
			Subsystem: apiserverSubsystem,
			Name:      "request_inflight",
			Help:      "Number of HTTP requests currently being processed by Clusterpedia APIServer, broken out by handler.",
		},
		[]string{"handler"},
	)
)

func init() {
	legacyregistry.MustRegister(requestTotal)
	legacyregistry.MustRegister(requestDuration)
	legacyregistry.MustRegister(responseSize)
	legacyregistry.MustRegister(requestInflight)
}

func IncInflight(handler string) {
	requestInflight.WithLabelValues(handler).Inc()
}

func DecInflight(handler string) {
	requestInflight.WithLabelValues(handler).Dec()
}

// RecordRequest records count, latency and response size for a finished request.
func RecordRequest(code int, method, handler string, duration time.Duration, size int) {
	codeStr := strconv.Itoa(code)
	requestTotal.WithLabelValues(codeStr, handler, method).Inc()
	requestDuration.WithLabelValues(codeStr, handler, method).Observe(duration.Seconds())
	responseSize.WithLabelValues(codeStr, handler, method).Observe(float64(size))
}
