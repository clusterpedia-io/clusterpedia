package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app/config"
	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app/options"
	kubestatemetrics "github.com/clusterpedia-io/clusterpedia/pkg/kube_state_metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func init() {
	runtime.Must(logsapi.AddFeatureGates(clusterpediafeature.MutableFeatureGate))

	runtime.Must(storage.LoadPlugins(os.Getenv("STORAGE_PLUGINS")))
}

func NewClusterSynchroManagerCommand(ctx context.Context) *cobra.Command {
	opts, _ := options.NewClusterSynchroManagerOptions()
	cmd := &cobra.Command{
		Use: "clustersynchro-manager",
		PersistentPreRunE: func(*cobra.Command, []string) error {
			// k8s.io/kubernetes/cmd/kube-controller-manager/app/controllermanager.go

			// silence client-go warnings.
			// clustersynchro-manager generically watches APIs (including deprecated ones),
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

			if err := Run(ctx, config); err != nil {
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

func Run(ctx context.Context, c *config.Config) error {
	synchromanager := synchromanager.NewManager(c.CRDClient, c.StorageFactory, c.ClusterSyncConfig, c.ShardingName)

	go func() {
		metrics.RunServer(c.MetricsServerConfig)
	}()

	if c.KubeMetricsServerConfig != nil {
		go func() {
			kubestatemetrics.RunServer(*c.KubeMetricsServerConfig, synchromanager)
		}()
	}

	if !c.LeaderElection.LeaderElect {
		synchromanager.Run(c.WorkerNumber, ctx.Done())
		return nil
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

	var done chan struct{}
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Name: c.LeaderElection.ResourceName,

		Lock:          rl,
		LeaseDuration: c.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: c.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   c.LeaderElection.RetryPeriod.Duration,

		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				done = make(chan struct{})
				defer close(done)

				stopCh := ctx.Done()
				synchromanager.Run(c.WorkerNumber, stopCh)
			},
			OnStoppedLeading: func() {
				klog.Info("leaderelection lost")
				if done != nil {
					<-done
				}
			},
		},
	})
	return nil
}
