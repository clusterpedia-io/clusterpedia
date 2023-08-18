package kubestatemetrics

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"k8s.io/kube-state-metrics/v2/pkg/allowdenylist"
	"k8s.io/kube-state-metrics/v2/pkg/metric"
	generator "k8s.io/kube-state-metrics/v2/pkg/metric_generator"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"
	"k8s.io/kube-state-metrics/v2/pkg/optin"
	"k8s.io/kube-state-metrics/v2/pkg/options"

	"github.com/clusterpedia-io/clusterpedia/pkg/scheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/storageconfig"
)

var (
	storageConfigFactory = storageconfig.NewStorageConfigFactory()
	hubGVRs              = make(map[schema.GroupVersionResource]schema.GroupVersionResource)
)

func init() {
	for gvr := range generators {
		config, err := storageConfigFactory.NewLegacyResourceConfig(gvr.GroupResource(), false)
		if err != nil {
			panic(err)
		}
		hubGVRs[config.StorageGroupResource.WithVersion(config.MemoryVersion.Version)] = gvr
	}
}

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

	var match func(interface{}) (bool, error)
	if len(config.NamespacesDenylist) != 0 {
		set := sets.NewString(config.NamespacesDenylist...)
		match = func(obj interface{}) (bool, error) {
			typ, err := meta.Accessor(obj)
			if err != nil {
				return false, err
			}
			return !set.Has(typ.GetNamespace()), nil
		}
	} else if len(config.Namespaces) != 0 {
		set := sets.NewString(config.Namespaces...)
		match = func(obj interface{}) (bool, error) {
			typ, err := meta.Accessor(obj)
			if err != nil {
				return false, err
			}
			return set.Has(typ.GetNamespace()), nil
		}
	}

	filter, err := NewFamilyGeneratorFilter(config)
	if err != nil {
		return nil, err
	}
	return &MetricsStoreBuilder{
		familyGeneratorFilter: filter,
		resources:             config.Resources,
		match:                 match,
	}, nil
}

type MetricsStoreBuilder struct {
	familyGeneratorFilter generator.FamilyGeneratorFilter

	resources options.ResourceSet
	match     func(obj interface{}) (bool, error)
}

func (builder *MetricsStoreBuilder) GetMetricStore(cluster string, resource schema.GroupVersionResource) *MetricsStore {
	if _, ok := builder.resources[resource.Resource]; !ok {
		return nil
	}
	if gvr, ok := rToGVR[resource.Resource]; !ok || gvr != resource {
		return nil
	}

	if !scheme.LegacyResourceScheme.IsGroupRegistered(resource.Group) {
		return nil
	}

	config, err := storageConfigFactory.NewLegacyResourceConfig(resource.GroupResource(), false)
	if err != nil {
		return nil
	}
	hub := config.StorageGroupResource.WithVersion(config.MemoryVersion.Version)
	metricsGVR, ok := hubGVRs[hub]
	if !ok {
		return nil
	}
	f := generators[metricsGVR]
	if f == nil {
		return nil
	}

	metricFamilies := generator.FilterFamilyGenerators(builder.familyGeneratorFilter, f(nil, nil))
	storage := metricsstore.NewMetricsStore(
		generator.ExtractMetricFamilyHeaders(metricFamilies),
		composeMetricGenFuncs(cluster, metricFamilies),
	)

	convertor := func(obj interface{}) (interface{}, error) {
		typ, err := meta.TypeAccessor(obj)
		if err != nil {
			return nil, err
		}

		if typ.GetAPIVersion() == metricsGVR.GroupVersion().String() {
			if _, ok := obj.(*unstructured.Unstructured); ok {
				return scheme.LegacyResourceScheme.ConvertToVersion(obj.(runtime.Object), metricsGVR.GroupVersion())
			}
			return obj, nil
		}

		hobj, err := scheme.LegacyResourceScheme.ConvertToVersion(obj.(runtime.Object), config.MemoryVersion)
		if err != nil {
			return nil, err
		}
		if metricsGVR.GroupVersion() == config.MemoryVersion {
			return hobj, nil
		}
		return scheme.LegacyResourceScheme.ConvertToVersion(hobj, metricsGVR.GroupVersion())
	}

	return &MetricsStore{
		MetricsStore: storage,
		convertor:    convertor,
		match:        builder.match,
	}
}

func composeMetricGenFuncs(cluster string, familyGens []generator.FamilyGenerator) func(obj interface{}) []metric.FamilyInterface {
	return func(obj interface{}) []metric.FamilyInterface {
		families := make([]metric.FamilyInterface, len(familyGens))
		for i, gen := range familyGens {
			family := gen.Generate(obj)
			for _, m := range family.Metrics {
				m.LabelKeys = append([]string{"cluster"}, m.LabelKeys...)
				m.LabelValues = append([]string{cluster}, m.LabelValues...)
			}
			families[i] = family
		}
		return families
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

type MetricsStore struct {
	*metricsstore.MetricsStore

	convertor func(obj interface{}) (interface{}, error)
	match     func(obj interface{}) (bool, error)
}

func (store *MetricsStore) Add(obj interface{}) error {
	if store.match != nil {
		matched, err := store.match(obj)
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
	}

	obj, err := store.convertor(obj)
	if err != nil {
		return err
	}

	return store.MetricsStore.Add(obj)
}

func (store *MetricsStore) Update(obj interface{}) error {
	if store.match != nil {
		matched, err := store.match(obj)
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
	}

	obj, err := store.convertor(obj)
	if err != nil {
		return err
	}

	return store.MetricsStore.Update(obj)
}

func (store *MetricsStore) Delete(obj interface{}) error {
	if store.match != nil {
		matched, err := store.match(obj)
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
	}

	return store.MetricsStore.Delete(obj)
}

func (store *MetricsStore) Replace(list []interface{}, rv string) error {
	objs := make([]interface{}, 0, len(list))
	for _, obj := range list {
		if store.match != nil {
			matched, err := store.match(obj)
			if err != nil {
				continue
			}
			if !matched {
				continue
			}
		}
		o, err := store.convertor(obj)
		if err != nil {
			return err
		}
		objs = append(objs, o)
	}

	return store.MetricsStore.Replace(objs, rv)
}
