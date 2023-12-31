package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	genericfeatures "k8s.io/apiserver/pkg/features"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"

	"github.com/clusterpedia-io/clusterpedia/cmd/apiserver/app/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager"
	"github.com/clusterpedia-io/clusterpedia/pkg/synchromanager/clustersynchro"
	clusterpediafeature "github.com/clusterpedia-io/clusterpedia/pkg/utils/feature"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func NewClusterPediaServerCommand(ctx context.Context) *cobra.Command {
	opts := options.NewServerOptions()

	cmd := &cobra.Command{
		Use: "binding-apiserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			// Activate logging as soon as possible, after that
			// show flags with the final logging configuration.
			if err := logsapi.ValidateAndApply(opts.Logs, clusterpediafeature.FeatureGate); err != nil {
				return err
			}
			cliflag.PrintFlags(cmd.Flags())

			config, err := opts.Config()
			if err != nil {
				return err
			}

			completedConfig := config.Complete()
			if completedConfig.ClientConfig == nil {
				return fmt.Errorf("CompletedConfig.New() called with config.ClientConfig == nil")
			}
			if completedConfig.StorageFactory == nil {
				return fmt.Errorf("CompletedConfig.New() called with config.StorageFactory == nil")
			}

			crdclient, err := versioned.NewForConfig(completedConfig.ClientConfig)
			if err != nil {
				return err
			}

			synchromanager := synchromanager.NewManager(crdclient, config.StorageFactory, clustersynchro.ClusterSyncConfig{}, "")
			go synchromanager.Run(1, ctx.Done())

			server, err := completedConfig.New()
			if err != nil {
				return err
			}

			if err := server.Run(ctx); err != nil {
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

func init() {
	runtime.Must(logsapi.AddFeatureGates(clusterpediafeature.MutableFeatureGate))

	// The feature gate `RemainingItemCount` should default to false
	// https://github.com/clusterpedia-io/clusterpedia/issues/196
	gates := clusterpediafeature.MutableFeatureGate.GetAll()
	gate := gates[genericfeatures.RemainingItemCount]
	gate.Default = false
	gates[genericfeatures.RemainingItemCount] = gate

	clusterpediafeature.MutableFeatureGate = featuregate.NewFeatureGate()
	runtime.Must(clusterpediafeature.MutableFeatureGate.Add(gates))
	clusterpediafeature.FeatureGate = clusterpediafeature.MutableFeatureGate

	runtime.Must(storage.LoadPlugins(os.Getenv("STORAGE_PLUGINS")))
}
