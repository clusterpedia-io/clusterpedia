package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/clusterpedia-io/clusterpedia/cmd/controller-manager/app/config"
	"github.com/clusterpedia-io/clusterpedia/cmd/controller-manager/app/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller/clusterimportpolicy"
	"github.com/clusterpedia-io/clusterpedia/pkg/controller/pediaclusterlifecycle"
	clientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/informers/externalversions"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro/informer"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func init() {
	utilruntime.Must(logsapi.AddFeatureGates(clusterpediafeature.MutableFeatureGate))
}

func NewControllerManagerCommand() *cobra.Command {
	opts, _ := options.NewControllerManagerOptions()
	cmd := &cobra.Command{
		Use: "controller-manager",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// k8s.io/kubernetes/cmd/kube-controller-manager/app/controllermanager.go

			// silence client-go warnings.
			// controller-manager generically watches APIs (including deprecated ones),
			// and CI ensures it works properly against matching kube-apiserver versions.
			restclient.SetDefaultWarningHandler(restclient.NoWarnings{})
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			// Activate logging as soon as possible, after that
			// show flags with the final logging configuration.
			if err := logsapi.ValidateAndApply(opts.Logs, clusterpediafeature.MutableFeatureGate); err != nil {
				return err
			}
			cliflag.PrintFlags(cmd.Flags())

			config, err := opts.Config()
			if err != nil {
				return err
			}

			if err := Run(config); err != nil {
				return err
			}
			return nil
		},
	}

	namedFlagSets := opts.Flags()
	verflag.AddFlags(namedFlagSets.FlagSet("global"))
	globalflag.AddGlobalFlags(namedFlagSets.FlagSet("global"), cmd.Name(), logs.SkipLoggingConfigurationFlags())
	clusterpediafeature.MutableFeatureGate.AddFlag(namedFlagSets.FlagSet("mutable feature gate"))

	fs := cmd.Flags()
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, namedFlagSets, cols)
	return cmd
}

func Run(c *config.Config) error {
	if !c.LeaderElection.LeaderElect {
		return run(c.Kubeconfig, wait.NeverStop)
	}

	id, err := os.Hostname()
	if err != nil {
		return err
	}
	id += "_" + string(uuid.NewUUID())

	rl, err := resourcelock.NewFromKubeconfig(
		c.LeaderElection.ResourceLock,
		c.LeaderElection.ResourceNamespace,
		c.LeaderElection.ResourceName,
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: c.EventRecorder,
		},
		c.Kubeconfig,
		c.LeaderElection.RenewDeadline.Duration,
	)
	if err != nil {
		return fmt.Errorf("failed to create resource lock: %w", err)
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Name: c.LeaderElection.ResourceName,

		Lock:          rl,
		LeaseDuration: c.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: c.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   c.LeaderElection.RetryPeriod.Duration,

		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				_ = run(c.Kubeconfig, wait.NeverStop)
			},
			OnStoppedLeading: func() {
				klog.Info("leaderelection lost")
			},
		},
	})
	return nil
}

func run(config *restclient.Config, stopCh <-chan struct{}) error {
	client, err := clientset.NewForConfig(config)
	if err != nil {
		return err
	}
	httpClient, err := restclient.HTTPClientFor(config)
	if err != nil {
		return err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(config, httpClient)
	if err != nil {
		return err
	}

	lwFactory, err := informer.NewDynamicListerWatcherFactory(config)
	if err != nil {
		return err
	}

	// The queues will be shared between the controllers and the dependentResourceManager, so create them first
	policyQueue := workqueue.NewNamedRateLimitingQueue(
		workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 10*time.Second),
		"policy",
	)
	lifecycleQueue := workqueue.NewNamedRateLimitingQueue(
		workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 10*time.Second),
		"lifecycle",
	)
	dependentResourceManager := controller.NewDependentResourceManager(policyQueue, lifecycleQueue, lwFactory)

	informerFactory := externalversions.NewSharedInformerFactory(client, 0)
	policyController, err := clusterimportpolicy.NewController(
		mapper,
		client,
		informerFactory.Policy().V1alpha1().ClusterImportPolicies(),
		informerFactory.Policy().V1alpha1().PediaClusterLifecycles(),
		policyQueue,
		dependentResourceManager,
	)
	if err != nil {
		return err
	}

	lifecycleController, err := pediaclusterlifecycle.NewController(
		client,
		informerFactory.Policy().V1alpha1().PediaClusterLifecycles(),
		informerFactory.Cluster().V1alpha2().PediaClusters(),
		lifecycleQueue,
		dependentResourceManager,
	)
	if err != nil {
		return err
	}

	informerFactory.Start(stopCh)

	klog.Info("wait for cache sync...")
	informersByStarted := make(map[bool][]string)
	for informerType, started := range informerFactory.WaitForCacheSync(stopCh) {
		informersByStarted[started] = append(informersByStarted[started], informerType.String())
	}
	if notStarted := informersByStarted[false]; len(notStarted) != 0 {
		klog.Errorf("%d informers not started yet: %v", len(notStarted), notStarted)
		return fmt.Errorf("informers not started")
	}
	klog.Infof("informer caches is synced: %v", informersByStarted[true])

	go policyController.Run(5, stopCh)
	go lifecycleController.Run(5, stopCh)

	<-stopCh
	return nil
}
