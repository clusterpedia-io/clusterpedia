package config

import (
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	componentbaseconfig "k8s.io/component-base/config"

	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	metrics "github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type Config struct {
	Kubeconfig    *restclient.Config
	CRDClient     *crdclientset.Clientset
	EventRecorder record.EventRecorder

	WorkerNumber            int
	MetricsServerConfig     metrics.Config
	KubeMetricsServerConfig *kubestatemetrics.ServerConfig
	MetricsStoreBuilder     *kubestatemetrics.MetricsStoreBuilder
	StorageFactory          storage.StorageFactory

	LeaderElection   componentbaseconfig.LeaderElectionConfiguration
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
}
