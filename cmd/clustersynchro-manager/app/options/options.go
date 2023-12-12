package options

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/component-base/config/options"
	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"

	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app/config"
	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	storageoptions "github.com/clusterpedia-io/clusterpedia/pkg/storage/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro"
)

const (
	ClusterSynchroManagerUserAgent = "cluster-synchro-manager"
)

type Options struct {
	Master     string
	Kubeconfig string

	LeaderElection   componentbaseconfig.LeaderElectionConfiguration
	ClientConnection componentbaseconfig.ClientConnectionConfiguration

	Logs             *logs.Options
	Storage          *storageoptions.StorageOptions
	Metrics          *metrics.Options
	KubeStateMetrics *kubestatemetrics.Options

	WorkerNumber            int // WorkerNumber is the number of worker goroutines
	PageSizeForResourceSync int64
	ShardingName            string
}

func NewClusterSynchroManagerOptions() (*Options, error) {
	var (
		leaderElection   componentbaseconfigv1alpha1.LeaderElectionConfiguration
		clientConnection componentbaseconfigv1alpha1.ClientConnectionConfiguration
	)
	componentbaseconfigv1alpha1.RecommendedDefaultLeaderElectionConfiguration(&leaderElection)
	componentbaseconfigv1alpha1.RecommendedDefaultClientConnectionConfiguration(&clientConnection)

	leaderElection.ResourceName = "clusterpedia-clustersynchro-manager"
	leaderElection.ResourceNamespace = "clusterpedia-system"
	leaderElection.ResourceLock = resourcelock.LeasesResourceLock

	clientConnection.ContentType = runtime.ContentTypeJSON

	var options Options

	// not need scheme.Convert
	if err := componentbaseconfigv1alpha1.Convert_v1alpha1_LeaderElectionConfiguration_To_config_LeaderElectionConfiguration(&leaderElection, &options.LeaderElection, nil); err != nil {
		return nil, err
	}
	if err := componentbaseconfigv1alpha1.Convert_v1alpha1_ClientConnectionConfiguration_To_config_ClientConnectionConfiguration(&clientConnection, &options.ClientConnection, nil); err != nil {
		return nil, err
	}

	options.Logs = logs.NewOptions()
	options.Storage = storageoptions.NewStorageOptions()
	options.Metrics = metrics.NewMetricsServerOptions()
	options.KubeStateMetrics = kubestatemetrics.NewOptions()

	options.WorkerNumber = 5
	return &options, nil
}

func (o *Options) Flags() cliflag.NamedFlagSets {
	var fss cliflag.NamedFlagSets

	genericfs := fss.FlagSet("generic")
	genericfs.StringVar(&o.ClientConnection.ContentType, "kube-api-content-type", o.ClientConnection.ContentType, "Content type of requests sent to apiserver.")
	genericfs.Float32Var(&o.ClientConnection.QPS, "kube-api-qps", o.ClientConnection.QPS, "QPS to use while talking with kubernetes apiserver.")
	genericfs.Int32Var(&o.ClientConnection.Burst, "kube-api-burst", o.ClientConnection.Burst, "Burst to use while talking with kubernetes apiserver.")
	genericfs.IntVar(&o.WorkerNumber, "worker-number", o.WorkerNumber, "The number of worker goroutines.")
	genericfs.StringVar(&o.ShardingName, "sharding-name", o.ShardingName, "The sharding name of manager.")

	syncfs := fss.FlagSet("resource sync")
	syncfs.Int64Var(&o.PageSizeForResourceSync, "page-size", o.PageSizeForResourceSync, "The requested chunk size of initial and resync watch lists for resource sync")

	options.BindLeaderElectionFlags(&o.LeaderElection, genericfs)

	fs := fss.FlagSet("misc")
	fs.StringVar(&o.Master, "master", o.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&o.Kubeconfig, "kubeconfig", o.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")

	logsapi.AddFlags(o.Logs, fss.FlagSet("logs"))

	o.Storage.AddFlags(fss.FlagSet("storage"))
	o.Metrics.AddFlags(fss.FlagSet("metrics server"))
	o.KubeStateMetrics.AddFlags(fss.FlagSet("kube state metrics"))
	return fss
}

func (o *Options) Validate() error {
	var errs []error
	errs = append(errs, o.Storage.Validate()...)
	errs = append(errs, o.Metrics.Validate()...)
	errs = append(errs, o.KubeStateMetrics.Validate()...)

	if o.WorkerNumber <= 0 {
		errs = append(errs, fmt.Errorf("worker-number must be greater than 0"))
	}
	return utilerrors.NewAggregate(errs)
}

func (o *Options) Config() (*config.Config, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}

	storagefactory, err := storage.NewStorageFactory(o.Storage.Name, o.Storage.ConfigPath)
	if err != nil {
		return nil, err
	}

	kubeconfig, err := clientcmd.BuildConfigFromFlags(o.Master, o.Kubeconfig)
	if err != nil {
		return nil, err
	}
	kubeconfig.ContentConfig.AcceptContentTypes = o.ClientConnection.AcceptContentTypes
	kubeconfig.ContentConfig.ContentType = o.ClientConnection.ContentType
	kubeconfig.QPS = o.ClientConnection.QPS
	kubeconfig.Burst = int(o.ClientConnection.Burst)

	client, err := clientset.NewForConfig(restclient.AddUserAgent(kubeconfig, ClusterSynchroManagerUserAgent))
	if err != nil {
		return nil, err
	}
	crdclient, err := crdclientset.NewForConfig(restclient.AddUserAgent(kubeconfig, ClusterSynchroManagerUserAgent))
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: client.CoreV1().Events("")})
	eventRecorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: ClusterSynchroManagerUserAgent})

	metricsConfig := o.Metrics.Config()
	metricsStoreBuilder, err := o.KubeStateMetrics.MetricsStoreBuilderConfig().New()
	if err != nil {
		return nil, err
	}
	kubeStateMetricsServerConfig := o.KubeStateMetrics.ServerConfig(metricsConfig)

	if o.ShardingName != "" {
		o.LeaderElection.ResourceName = fmt.Sprintf("%s-%s", o.LeaderElection.ResourceName, o.ShardingName)
	}

	return &config.Config{
		CRDClient:     crdclient,
		Kubeconfig:    kubeconfig,
		EventRecorder: eventRecorder,

		StorageFactory: storagefactory,
		WorkerNumber:   o.WorkerNumber,
		ShardingName:   o.ShardingName,

		MetricsServerConfig:     metricsConfig,
		KubeMetricsServerConfig: kubeStateMetricsServerConfig,

		ClusterSyncConfig: clustersynchro.ClusterSyncConfig{
			MetricsStoreBuilder:     metricsStoreBuilder,
			PageSizeForResourceSync: o.PageSizeForResourceSync,
		},

		LeaderElection: o.LeaderElection,
	}, nil
}
