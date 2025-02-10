package clustersynchro

import (
	"sync"

	compbasemetrics "k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"

	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/resourcesynchro"
)

// Note: The current design pattern does not account for the presence of multiple different types of resource synchronizers simultaneously.
// Future updates can be made based on requirements.

const (
	namespace = "clustersynchro"
	subsystem = "resourcesynchro"
)

var (
	// storagedResourcesTotal records the total number of resources stored in the storage layer.
	storagedResourcesTotal *compbasemetrics.GaugeVec

	// resourceAddedCounter records the number of times resources are added to the storage layer.
	resourceAddedCounter *compbasemetrics.CounterVec

	// resourceUpdatedCounter records the number of times resources are updated in the storage layer.
	resourceUpdatedCounter *compbasemetrics.CounterVec

	// resourceDeletedCounter records the number of times resources are deleted from the storage layer.
	resourceDeletedCounter *compbasemetrics.CounterVec

	// resourceFailedCounter records the number of times resource operations fail.
	resourceFailedCounter *compbasemetrics.CounterVec

	// resourceDroppedCounter records the number of times resources are dropped.
	resourceDroppedCounter *compbasemetrics.CounterVec

	// resourceMaxRetryGauge provides the maximum number of retries during resource operations.
	resourceMaxRetryGauge *compbasemetrics.GaugeVec

	// resourceStorageDuration records the time interval from when a resource is fetched from the queue to when it is processed.
	resourceStorageDuration *compbasemetrics.HistogramVec
)

var resourceSynchroMetrics = []interface{}{
	storagedResourcesTotal,
	resourceAddedCounter,
	resourceUpdatedCounter,
	resourceDeletedCounter,
	resourceFailedCounter,
	resourceMaxRetryGauge,
	resourceDroppedCounter,
	resourceStorageDuration,
}

var registerOnce sync.Once

func registerResourceSynchroMetrics() {
	registerOnce.Do(func() {
		storagedResourcesTotal = resourcesynchro.DefaultMetricsWrapperFactory.NewGaugeVec(
			&compbasemetrics.GaugeOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "storaged_resource_total",
				Help:           "Number of resources stored in the storage layer.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceAddedCounter = resourcesynchro.DefaultMetricsWrapperFactory.NewCounterVec(
			&compbasemetrics.CounterOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "resource_added_total",
				Help:           "Number of times resources are deleted from the storage layer.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceUpdatedCounter = resourcesynchro.DefaultMetricsWrapperFactory.NewCounterVec(
			&compbasemetrics.CounterOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "resource_updated_total",
				Help:           "Number of times resources are updated in the storage layer.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceDeletedCounter = resourcesynchro.DefaultMetricsWrapperFactory.NewCounterVec(
			&compbasemetrics.CounterOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "resource_deleted_total",
				Help:           "Number of times resources are deleted from the storage layer.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceFailedCounter = resourcesynchro.DefaultMetricsWrapperFactory.NewCounterVec(
			&compbasemetrics.CounterOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "resource_failed_total",
				Help:           "Number of times resource operations fail.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceMaxRetryGauge = resourcesynchro.DefaultMetricsWrapperFactory.NewGaugeVec(
			&compbasemetrics.GaugeOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "resource_max_retry_total",
				Help:           "The maximum number of retries during resource operations.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceDroppedCounter = resourcesynchro.DefaultMetricsWrapperFactory.NewCounterVec(
			&compbasemetrics.CounterOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "failed_resource_total",
				Help:           "Number of times resources are dropped.",
				StabilityLevel: compbasemetrics.ALPHA,
			},
		)

		resourceStorageDuration = resourcesynchro.DefaultMetricsWrapperFactory.NewHistogramVec(
			&compbasemetrics.HistogramOpts{
				Namespace:      namespace,
				Subsystem:      subsystem,
				Name:           "storage_duration_seconds",
				Help:           "The time interval from when a resource is fetched from the queue to when it is processed.",
				StabilityLevel: compbasemetrics.ALPHA,
				Buckets:        []float64{0.025, 0.05, 0.1, 0.2, 0.4, 0.6, 1.0, 1.5, 3, 5, 8, 15},
			},
		)

		resourceSynchroMetrics = []interface{}{
			storagedResourcesTotal,
			resourceAddedCounter,
			resourceUpdatedCounter,
			resourceDeletedCounter,
			resourceFailedCounter,
			resourceMaxRetryGauge,
			resourceDroppedCounter,
			resourceStorageDuration,
		}
		for _, m := range resourceSynchroMetrics {
			legacyregistry.MustRegister(m.(compbasemetrics.Registerable))
		}
	})
}
