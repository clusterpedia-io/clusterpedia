package kubestatemetrics

import (
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/kube-state-metrics/v2/pkg/options"

	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
)

var defaultResources = options.ResourceSet{
	"pods":        struct{}{},
	"deployments": struct{}{},
	"services":    struct{}{},
}

type Options struct {
	EnableKubeStateMetrics bool

	Host string
	Port int

	MetricAllowlist options.MetricSet
	MetricDenylist  options.MetricSet
	MetricOptInList options.MetricSet

	Resources          options.ResourceSet
	Namespaces         options.NamespaceList
	NamespacesDenylist options.NamespaceList
}

func NewOptions() *Options {
	return &Options{
		EnableKubeStateMetrics: false,

		Host: "::",
		Port: 8080,

		MetricAllowlist: options.MetricSet{},
		MetricDenylist:  options.MetricSet{},
		MetricOptInList: options.MetricSet{},

		Resources: defaultResources,
	}
}

func (o *Options) Validate() []error {
	return nil
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	var resources []string
	for r := range rToGVR {
		resources = append(resources, r)
	}
	sort.Strings(resources)

	fs.BoolVar(&o.EnableKubeStateMetrics, "enable-kube-state-metrics", o.EnableKubeStateMetrics, "Enabled kube state metrics")
	fs.IntVar(&o.Port, "kube-state-metrics-port", o.Port, "Port to expose kube state metrics on.")
	fs.StringVar(&o.Host, "kube-state-metrics-host", o.Host, "Host to expose kube state metrics on.")

	fs.Var(&o.MetricAllowlist, "kube-state-metrics-metric-allowlist", "Comma-separated list of metrics to be exposed. This list comprises of exact metric names and/or regex patterns. The allowlist and denylist are mutually exclusive.")
	fs.Var(&o.MetricDenylist, "kube-state-metrics-metric-denylist", "Comma-separated list of metrics not to be enabled. This list comprises of exact metric names and/or regex patterns. The allowlist and denylist are mutually exclusive.")
	fs.Var(&o.MetricOptInList, "kube-state-metrics-metric-opt-in-list", "Comma-separated list of metrics which are opt-in and not enabled by default. This is in addition to the metric allow- and denylists")

	fs.Var(&o.Resources, "kube-state-metrics-resources", fmt.Sprintf("Comma-separated list of Resources to be enabled. Supported resources: %q", strings.Join(resources, ",")))
	fs.Var(&o.Namespaces, "kube-state-metrics-namespaces", fmt.Sprintf("Comma-separated list of namespaces to be enabled. Defaults to %q", &o.Namespaces))
	fs.Var(&o.NamespacesDenylist, "kube-state-metrics-namespaces-denylist", "Comma-separated list of namespaces not to be enabled. If namespaces and namespaces-denylist are both set, only namespaces that are excluded in namespaces-denylist will be used.")
}

func (o *Options) MetricsStoreBuilderConfig() *MetricsStoreBuilderConfig {
	if !o.EnableKubeStateMetrics {
		return nil
	}
	return &MetricsStoreBuilderConfig{
		MetricAllowlist:    o.MetricAllowlist,
		MetricDenylist:     o.MetricDenylist,
		MetricOptInList:    o.MetricOptInList,
		Resources:          o.Resources,
		Namespaces:         o.Namespaces,
		NamespacesDenylist: o.NamespacesDenylist,
	}
}

func (o *Options) ServerConfig(config metrics.Config) *ServerConfig {
	if !o.EnableKubeStateMetrics {
		return nil
	}
	return &ServerConfig{
		Endpoint:            net.JoinHostPort(o.Host, strconv.Itoa(o.Port)),
		DisableGZIPEncoding: config.DisableGZIPEncoding,
		TLSConfig:           config.TLSConfig,
	}
}
