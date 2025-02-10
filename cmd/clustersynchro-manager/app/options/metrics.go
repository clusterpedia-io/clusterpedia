package options

import (
	"github.com/spf13/pflag"

	metricsserver "github.com/clusterpedia-io/clusterpedia/pkg/metrics/server"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/resourcesynchro"
)

type MetricsOptions struct {
	Metrics *metricsserver.Options

	ResourceSynchroMetricsLabels string
}

func NewMetricsOptions() *MetricsOptions {
	return &MetricsOptions{
		Metrics:                      metricsserver.NewOptions(),
		ResourceSynchroMetricsLabels: resourcesynchro.DefaultMetricsWrapperFactory.Config().ScopeLabels(),
	}
}

func (o *MetricsOptions) AddFlags(fs *pflag.FlagSet) {
	o.Metrics.AddFlags(fs)

	fs.StringVar(&o.ResourceSynchroMetricsLabels, "resource-synchro-metrics-labels", o.ResourceSynchroMetricsLabels, "The resource synchronizer's metrics aggregation scope, which supports 'empty', 'cluster', 'gv','gvr', 'cluster,gv', 'cluster,gvr', etc.")
}

func (o *MetricsOptions) Validate() []error {
	errs := o.Metrics.Validate()

	if _, err := resourcesynchro.ParseToMetricsWrapperConfig(o.ResourceSynchroMetricsLabels); err != nil {
		errs = append(errs, err)
	}
	return errs
}

func (o *MetricsOptions) ServerConfig() metricsserver.Config {
	return o.Metrics.Config()
}

func (o *MetricsOptions) ResourceSynchroConfig() resourcesynchro.MetricsWrapperConfig {
	config, err := resourcesynchro.ParseToMetricsWrapperConfig(o.ResourceSynchroMetricsLabels)
	if err != nil {
		panic(err)
	}
	return config
}
