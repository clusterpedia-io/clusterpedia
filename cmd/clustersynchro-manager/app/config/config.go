package config

import (
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	componentbaseconfig "k8s.io/component-base/config"

	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	metricsserver "github.com/clusterpedia-io/clusterpedia/pkg/metrics/server"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro"
)

type Config struct {
	Kubeconfig    *restclient.Config
	Namespace     string
	Client        kubernetes.Interface
	CRDClient     *crdclientset.Clientset
	EventRecorder record.EventRecorder

	WorkerNumber            int
	ShardingName            string
	MetricsServerConfig     metricsserver.Config
	KubeMetricsServerConfig *kubestatemetrics.ServerConfig
	StorageFactory          storage.StorageFactory
	ClusterSyncConfig       clustersynchro.ClusterSyncConfig

	LeaderElection   componentbaseconfig.LeaderElectionConfiguration
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
}
