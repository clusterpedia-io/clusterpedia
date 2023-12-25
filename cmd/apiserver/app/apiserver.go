package app

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/term"

	"github.com/clusterpedia-io/clusterpedia/cmd/apiserver/app/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func NewClusterPediaServerCommand(ctx context.Context) *cobra.Command {
	opts := options.NewServerOptions()

	cmd := &cobra.Command{
		Use: "clusterpedia-apiserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()

			// Activate logging as soon as possible, after that
			// show flags with the final logging configuration.
			if err := logsapi.ValidateAndApply(opts.Logs, utilfeature.DefaultFeatureGate); err != nil {
				return err
			}
			cliflag.PrintFlags(cmd.Flags())

			config, err := opts.Config()
			if err != nil {
				return err
			}

			server, err := config.Complete().New()
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
	utilfeature.DefaultMutableFeatureGate.AddFlag(namedFlagSets.FlagSet("mutable feature gate"))

	fs := cmd.Flags()
	for _, f := range namedFlagSets.FlagSets {
		fs.AddFlagSet(f)
	}

	cols, _, _ := term.TerminalSize(cmd.OutOrStdout())
	cliflag.SetUsageAndHelpFunc(cmd, namedFlagSets, cols)
	return cmd
}

var wrapedDefaultFeatureGate = map[featuregate.Feature]bool{
	// https://github.com/clusterpedia-io/clusterpedia/issues/196
	genericfeatures.RemainingItemCount: false,

	genericfeatures.APIServerTracing: false,
}

func init() {
	runtime.Must(logsapi.AddFeatureGates(utilfeature.DefaultMutableFeatureGate))

	gates := utilfeature.DefaultMutableFeatureGate.GetAll()
	for feature, value := range wrapedDefaultFeatureGate {
		gate, ok := gates[feature]
		if !ok {
			panic(fmt.Sprintf("FeatureGate[%s] is not existed in the default feature gates", feature))
		}
		gate.Default = value
		gates[feature] = gate
	}

	utilfeature.DefaultMutableFeatureGate = featuregate.NewFeatureGate()
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(gates))
	utilfeature.DefaultFeatureGate = utilfeature.DefaultMutableFeatureGate

	runtime.Must(storage.LoadPlugins(os.Getenv("STORAGE_PLUGINS")))
}
