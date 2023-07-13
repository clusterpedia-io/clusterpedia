package kubestatemetrics

import (
	"fmt"
	_ "unsafe"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"k8s.io/kube-state-metrics/v2/pkg/allowdenylist"
	_ "k8s.io/kube-state-metrics/v2/pkg/builder"
	"k8s.io/kube-state-metrics/v2/pkg/metric"
	generator "k8s.io/kube-state-metrics/v2/pkg/metric_generator"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"
	"k8s.io/kube-state-metrics/v2/pkg/optin"
	"k8s.io/kube-state-metrics/v2/pkg/options"
)

type MetricsStoreBuilderConfig struct {
	MetricAllowlist options.MetricSet
	MetricDenylist  options.MetricSet
	MetricOptInList options.MetricSet

	Resources          options.ResourceSet
	Namespaces         options.NamespaceList
	NamespacesDenylist options.NamespaceList
}

func (config *MetricsStoreBuilderConfig) New() (*MetricsStoreBuilder, error) {
	if config == nil {
		return nil, nil
	}

	filter, err := NewFamilyGeneratorFilter(config)
	if err != nil {
		return nil, err
	}
	return &MetricsStoreBuilder{
		familyGeneratorFilter: filter,
	}, nil
}

type MetricsStoreBuilder struct {
	familyGeneratorFilter generator.FamilyGeneratorFilter
}

func (builder *MetricsStoreBuilder) GetMetricStore(cluster string, resource schema.GroupVersionResource) *metricsstore.MetricsStore {
	metricFamilies := generators[resource]
	if len(metricFamilies) == 0 {
		return nil
	}
	metricFamilies = generator.FilterFamilyGenerators(builder.familyGeneratorFilter, metricFamilies)
	return metricsstore.NewMetricsStore(
		generator.ExtractMetricFamilyHeaders(metricFamilies),
		composeMetricGenFuncs(cluster, metricFamilies),
	)
}

func composeMetricGenFuncs(cluster string, familyGens []generator.FamilyGenerator) func(obj interface{}) []metric.FamilyInterface {
	return func(obj interface{}) []metric.FamilyInterface {
		familes := make([]metric.FamilyInterface, len(familyGens))
		for i, gen := range familyGens {
			family := gen.Generate(obj)
			for _, m := range family.Metrics {
				m.LabelKeys = append([]string{"cluster"}, m.LabelKeys...)
				m.LabelValues = append([]string{cluster}, m.LabelValues...)
			}
			familes[i] = family
		}
		return familes
	}
}

func NewFamilyGeneratorFilter(config *MetricsStoreBuilderConfig) (generator.FamilyGeneratorFilter, error) {
	allowDenyList, err := allowdenylist.New(config.MetricAllowlist, config.MetricDenylist)
	if err != nil {
		return nil, err
	}

	err = allowDenyList.Parse()
	if err != nil {
		return nil, fmt.Errorf("error initializing the allowdeny list: %v", err)
	}

	klog.InfoS("Metric allow-denylisting", "allowDenyStatus", allowDenyList.Status())

	optInMetricFamilyFilter, err := optin.NewMetricFamilyFilter(config.MetricOptInList)
	if err != nil {
		return nil, fmt.Errorf("error initializing the opt-in metric list: %v", err)
	}

	if optInMetricFamilyFilter.Count() > 0 {
		klog.InfoS("Metrics which were opted into", "optInMetricsFamilyStatus", optInMetricFamilyFilter.Status())
	}

	return generator.NewCompositeFamilyGeneratorFilter(
		allowDenyList,
		optInMetricFamilyFilter,
	), nil
}
