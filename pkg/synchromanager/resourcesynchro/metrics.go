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

	sumLock    sync.Mutex
	sumMetrics map[*metrics.GaugeVec]*sum

	maxLock    sync.Mutex
	maxMetrics map[*metrics.GaugeVec]*max
}

func NewMetricsWrapperFactory(config MetricsWrapperConfig) *MetricsWrapperFactory {
	return &MetricsWrapperFactory{
		MetricsWrapperConfig: config,
		sumMetrics:           make(map[*metrics.GaugeVec]*sum),
		maxMetrics:           make(map[*metrics.GaugeVec]*max),
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
	return metrics.NewCounterVec(opts, c.labels())
}

func (c *MetricsWrapperFactory) NewGaugeVec(opts *metrics.GaugeOpts) *metrics.GaugeVec {
	return metrics.NewGaugeVec(opts, c.labels())
}

func (c *MetricsWrapperFactory) NewHistogramVec(opts *metrics.HistogramOpts) *metrics.HistogramVec {
	return metrics.NewHistogramVec(opts, c.labels())
}

func (c *MetricsWrapperFactory) NewWrapper(cluster string, gvr schema.GroupVersionResource) MetricsWrapper {
	scopeKey := ""
	labels := prometheus.Labels{}
	if c.Cluster {
		scopeKey += cluster
		labels["cluster"] = cluster
	}
	if c.GroupVersion {
		scopeKey += " " + gvr.GroupVersion().String()
		labels["group"] = gvr.Group
		labels["version"] = gvr.Version
	}
	if c.Resource {
		scopeKey += " " + gvr.Resource
		labels["resource"] = gvr.Resource
	}
	return MetricsWrapper{c: c, scopeKey: scopeKey, key: cluster + gvr.String(), labels: labels}
}

type MetricsWrapper struct {
	c *MetricsWrapperFactory

	key      string
	scopeKey string
	labels   prometheus.Labels
}

type DeletableVecMetrics interface {
	Delete(map[string]string) bool
}

func (u *MetricsWrapper) Delete(v DeletableVecMetrics) {
	v.Delete(u.labels)

	if g, ok := v.(*metrics.GaugeVec); ok {
		u.c.sumLock.Lock()
		sum := u.c.sumMetrics[g]
		u.c.sumLock.Unlock()
		if sum != nil {
			sum.delete(u.scopeKey, u.key)
		}
	}
}

func (u *MetricsWrapper) Counter(m *metrics.CounterVec) metrics.CounterMetric {
	return m.With(u.labels)
}

func (u *MetricsWrapper) Gauge(m *metrics.GaugeVec) metrics.GaugeMetric {
	return m.With(u.labels)
}

func (u *MetricsWrapper) Historgram(m *metrics.HistogramVec) metrics.ObserverMetric {
	return m.With(u.labels)
}

func (u *MetricsWrapper) Sum(m *metrics.GaugeVec, value float64) {
	metric := u.Gauge(m)
	if u.c.Cluster && u.c.GroupVersion && u.c.Resource {
		metric.Set(value)
		return
	}

	u.c.sumLock.Lock()
	defer u.c.sumLock.Unlock()
	s := u.c.sumMetrics[m]
	if s == nil {
		s = &sum{
			totals: make(map[string]float64),
			maps:   make(map[string]float64),
		}
		u.c.sumMetrics[m] = s
	}

	metric.Set(s.set(u.scopeKey, u.key, value))
}

func (u *MetricsWrapper) Max(m *metrics.GaugeVec, value float64) {
	metric := u.Gauge(m)
	if u.c.Cluster && u.c.GroupVersion && u.c.Resource {
		metric.Set(value)
		return
	}

	u.c.maxLock.Lock()
	defer u.c.maxLock.Unlock()
	maxs := u.c.maxMetrics[m]
	if maxs == nil {
		maxs = &max{
			maxs: make(map[string]float64),
		}
		u.c.maxMetrics[m] = maxs
	}

	mx := maxs.maxs[u.scopeKey]
	if value > mx {
		maxs.maxs[u.scopeKey] = value
		metric.Set(value)
	}
}

type max struct {
	maxs map[string]float64
}

type sum struct {
	// sync.Mutex

	totals map[string]float64
	maps   map[string]float64
}

func (s *sum) set(totalKey, key string, value float64) float64 {
	if last, ok := s.maps[key]; ok {
		s.totals[totalKey] -= last
	}
	s.totals[totalKey] += value
	s.maps[key] = value
	return s.totals[totalKey]
}

func (s *sum) delete(totalKey, key string) float64 {
	if last, ok := s.maps[key]; ok {
		s.totals[totalKey] -= last
		delete(s.maps, key)
	}
	return s.totals[totalKey]
}
