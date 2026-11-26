package resourcesynchro

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/component-base/metrics"
)

func TestMetricsWrapperLifecycle(t *testing.T) {
	deployments := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}

	type values struct{ added, stored, maxRetry float64 }
	// keyed by the label values of the series
	type series map[string]values

	steps := [5]string{
		"c1 deployments, c1 pods and c2 pods reported 1, 4 and 2",
		"c1 pods closed",
		"c1 pods recreated and reported 3",
		"c1 closed and deleted",
		"c1 deployments recreated and reported 1",
	}
	tests := []struct {
		labels   string
		expected [len(steps)]series
	}{
		{"empty", [len(steps)]series{
			{"": {7, 7, 4}},
			{"": {7, 3, 4}},
			{"": {10, 6, 4}},
			{"": {10, 2, 4}},
			{"": {11, 3, 4}},
		}},
		{"cluster", [len(steps)]series{
			{"c1": {5, 5, 4}, "c2": {2, 2, 2}},
			{"c1": {5, 1, 4}, "c2": {2, 2, 2}},
			{"c1": {8, 4, 4}, "c2": {2, 2, 2}},
			{"c2": {2, 2, 2}},
			{"c1": {1, 1, 1}, "c2": {2, 2, 2}},
		}},
		{"cluster,gvr", [len(steps)]series{
			{"c1,apps,deployments,v1": {1, 1, 1}, "c1,,pods,v1": {4, 4, 4}, "c2,,pods,v1": {2, 2, 2}},
			{"c1,apps,deployments,v1": {1, 1, 1}, "c1,,pods,v1": {4, 0, 4}, "c2,,pods,v1": {2, 2, 2}},
			{"c1,apps,deployments,v1": {1, 1, 1}, "c1,,pods,v1": {7, 3, 4}, "c2,,pods,v1": {2, 2, 2}},
			{"c2,,pods,v1": {2, 2, 2}},
			{"c1,apps,deployments,v1": {1, 1, 1}, "c2,,pods,v1": {2, 2, 2}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.labels, func(t *testing.T) {
			config, err := ParseToMetricsWrapperConfig(tt.labels)
			require.NoError(t, err)
			factory := NewMetricsWrapperFactory(config)

			added := factory.NewCounterVec(&metrics.CounterOpts{Name: "added"})
			stored := factory.NewGaugeVec(&metrics.GaugeOpts{Name: "stored"})
			maxRetry := factory.NewGaugeVec(&metrics.GaugeOpts{Name: "max_retry"})
			registry := metrics.NewKubeRegistry()
			registry.MustRegister(added, stored, maxRetry)

			report := func(cluster string, gvr schema.GroupVersionResource, value float64) MetricsWrapper {
				wrapper := factory.NewWrapper(cluster, gvr)
				wrapper.Counter(added).Add(value)
				wrapper.Sum(stored, value)
				wrapper.Max(maxRetry, value)
				return wrapper
			}
			assertStep := func(step int) {
				families, err := registry.Gather()
				require.NoError(t, err)
				actual := series{}
				for _, family := range families {
					for _, metric := range family.GetMetric() {
						var labels []string
						for _, label := range metric.GetLabel() {
							labels = append(labels, label.GetValue())
						}
						key := strings.Join(labels, ",")
						v := actual[key]
						switch family.GetName() {
						case "added":
							v.added = metric.GetCounter().GetValue()
						case "stored":
							v.stored = metric.GetGauge().GetValue()
						case "max_retry":
							v.maxRetry = metric.GetGauge().GetValue()
						}
						actual[key] = v
					}
				}
				assert.Equal(t, tt.expected[step], actual, steps[step])
			}

			c1Deployments := report("c1", deployments, 1)
			c1Pods := report("c1", pods, 4)
			report("c2", pods, 2)
			assertStep(0)

			c1Pods.Close()
			assertStep(1)

			c1Pods = report("c1", pods, 3)
			assertStep(2)

			c1Deployments.Close()
			c1Pods.Close()
			factory.DeleteCluster("c1")
			assertStep(3)

			report("c1", deployments, 1)
			assertStep(4)
		})
	}
}
