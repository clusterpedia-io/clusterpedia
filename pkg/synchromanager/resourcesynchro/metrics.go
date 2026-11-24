package resourcesynchro

import (
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/component-base/metrics"
)

// DefaultMetricsWrapperFactory allows users to initialize globally,
// it does not add extra functions or validations and can be set directly during the initialization phase.
var DefaultMetricsWrapperFactory = NewMetricsWrapperFactory(MetricsWrapperConfig{
	Cluster:      false,
	GroupVersion: false,
	Resource:     false,
})

type MetricsWrapperConfig struct {
	Cluster      bool
	GroupVersion bool
	Resource     bool
}

func ParseToMetricsWrapperConfig(labels string) (MetricsWrapperConfig, error) {
	if labels == "" {
		return DefaultMetricsWrapperFactory.MetricsWrapperConfig, nil
	}

	if labels == "empty" {
		return MetricsWrapperConfig{}, nil
	}

	var config MetricsWrapperConfig
	for _, l := range strings.Split(labels, ",") {
		switch l {
		case "cluster":
			config.Cluster = true
		case "groupversion", "gv":
			config.GroupVersion = true
		case "resource":
			config.Resource = true
		case "gvr":
			config.GroupVersion = true
			config.Resource = true
		default:
			return MetricsWrapperConfig{}, fmt.Errorf("resource synchro metrics scope '%s' is not supported", l)
		}
	}
	return config, nil
}

func (c MetricsWrapperConfig) ScopeLabels() string {
	var l []string
	if c.Cluster {
		l = append(l, "cluster")
	}
	if c.GroupVersion {
		l = append(l, "groupversion")
	}
	if c.Resource {
		l = append(l, "resource")
	}
	if len(l) == 0 {
		return "empty"
	}
	return strings.Join(l, ",")
}

type MetricsWrapperFactory struct {
	MetricsWrapperConfig

	lock   sync.Mutex
	vecs   []seriesDeleter
	scopes map[metricsScopeKey]*metricsScope
}

type seriesDeleter interface {
	Delete(map[string]string) bool
}

// metricsScopeKey holds the values of the configured labels, the others stay empty.
type metricsScopeKey struct {
	cluster string
	gvr     schema.GroupVersionResource
}

// metricsScope is one label combination, its series are shared by every wrapper created for it.
type metricsScope struct {
	labels prometheus.Labels
	sums   map[*metrics.GaugeVec]float64
	maxs   map[*metrics.GaugeVec]float64
}

func NewMetricsWrapperFactory(config MetricsWrapperConfig) *MetricsWrapperFactory {
	return &MetricsWrapperFactory{
		MetricsWrapperConfig: config,
		scopes:               make(map[metricsScopeKey]*metricsScope),
	}
}

func (c *MetricsWrapperFactory) Config() MetricsWrapperConfig {
	return c.MetricsWrapperConfig
}

func (c *MetricsWrapperFactory) labels() []string {
	var labels []string
	if c.Cluster {
		labels = append(labels, "cluster")
	}
	if c.GroupVersion {
		labels = append(labels, "group")
		labels = append(labels, "version")
	}
	if c.Resource {
		labels = append(labels, "resource")
	}
	return labels
}

func (c *MetricsWrapperFactory) NewCounterVec(opts *metrics.CounterOpts) *metrics.CounterVec {
	vec := metrics.NewCounterVec(opts, c.labels())
	c.track(vec)
	return vec
}

func (c *MetricsWrapperFactory) NewGaugeVec(opts *metrics.GaugeOpts) *metrics.GaugeVec {
	vec := metrics.NewGaugeVec(opts, c.labels())
	c.track(vec)
	return vec
}

func (c *MetricsWrapperFactory) NewHistogramVec(opts *metrics.HistogramOpts) *metrics.HistogramVec {
	vec := metrics.NewHistogramVec(opts, c.labels())
	c.track(vec)
	return vec
}

func (c *MetricsWrapperFactory) track(vec seriesDeleter) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.vecs = append(c.vecs, vec)
}

func (c *MetricsWrapperFactory) NewWrapper(cluster string, gvr schema.GroupVersionResource) MetricsWrapper {
	var key metricsScopeKey
	labels := prometheus.Labels{}
	if c.Cluster {
		key.cluster = cluster
		labels["cluster"] = cluster
	}
	if c.GroupVersion {
		key.gvr.Group, key.gvr.Version = gvr.Group, gvr.Version
		labels["group"] = gvr.Group
		labels["version"] = gvr.Version
	}
	if c.Resource {
		key.gvr.Resource = gvr.Resource
		labels["resource"] = gvr.Resource
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	scope := c.scopes[key]
	if scope == nil {
		scope = &metricsScope{
			labels: labels,
			sums:   make(map[*metrics.GaugeVec]float64),
			maxs:   make(map[*metrics.GaugeVec]float64),
		}
		c.scopes[key] = scope
	}
	return MetricsWrapper{c: c, scope: scope, sums: make(map[*metrics.GaugeVec]float64)}
}

// DeleteCluster deletes the series labeled with the cluster, scopes without the cluster label never match.
func (c *MetricsWrapperFactory) DeleteCluster(cluster string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	for key, scope := range c.scopes {
		if key.cluster != cluster {
			continue
		}
		for _, vec := range c.vecs {
			vec.Delete(scope.labels)
		}
		delete(c.scopes, key)
	}
}

type MetricsWrapper struct {
	c     *MetricsWrapperFactory
	scope *metricsScope

	// sums are the values the wrapper currently adds to the sum gauges of its scope
	sums map[*metrics.GaugeVec]float64
}

// Close takes back what the wrapper adds to the sum gauges, every other value outlives it.
func (u *MetricsWrapper) Close() {
	u.c.lock.Lock()
	defer u.c.lock.Unlock()

	for m, value := range u.sums {
		u.scope.sums[m] -= value
		u.Gauge(m).Set(u.scope.sums[m])
	}
}

func (u *MetricsWrapper) Counter(m *metrics.CounterVec) metrics.CounterMetric {
	return m.With(u.scope.labels)
}

func (u *MetricsWrapper) Gauge(m *metrics.GaugeVec) metrics.GaugeMetric {
	return m.With(u.scope.labels)
}

func (u *MetricsWrapper) Historgram(m *metrics.HistogramVec) metrics.ObserverMetric {
	return m.With(u.scope.labels)
}

func (u *MetricsWrapper) Sum(m *metrics.GaugeVec, value float64) {
	u.c.lock.Lock()
	defer u.c.lock.Unlock()

	u.scope.sums[m] += value - u.sums[m]
	u.sums[m] = value
	u.Gauge(m).Set(u.scope.sums[m])
}

func (u *MetricsWrapper) Max(m *metrics.GaugeVec, value float64) {
	u.c.lock.Lock()
	defer u.c.lock.Unlock()

	if value > u.scope.maxs[m] {
		u.scope.maxs[m] = value
		u.Gauge(m).Set(value)
	}
}
