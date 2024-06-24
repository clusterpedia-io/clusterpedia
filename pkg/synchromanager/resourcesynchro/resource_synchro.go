package resourcesynchro

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	metricsstore "k8s.io/kube-state-metrics/v2/pkg/metrics_store"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type SynchroFactory interface {
	NewResourceSynchro(name string, config Config) (Synchro, error)
}

type Synchro interface {
	Run(shutdown <-chan struct{})
	Start(stopCh <-chan struct{})
	Close() <-chan struct{}

	GroupVersionResource() schema.GroupVersionResource
	StoragedGroupVersionResource() schema.GroupVersionResource

	GetMetricsWriter() *metricsstore.MetricsWriter

	// TODO(iceber): Provide a more meaningful name for this method
	Stage() string
	Status() clusterv1alpha2.ClusterResourceSyncCondition
}

type Config struct {
	schema.GroupVersionResource
	Kind string

	ListerWatcher   cache.ListerWatcher
	ObjectConvertor runtime.ObjectConvertor

	ResourceVersions    map[string]interface{}
	PageSizeForInformer int64

	MetricsStore    *kubestatemetrics.MetricsStore
	ResourceStorage storage.ResourceStorage
}

func (c Config) GroupVersionKind() schema.GroupVersionKind {
	return c.GroupVersionResource.GroupVersion().WithKind(c.Kind)
}
