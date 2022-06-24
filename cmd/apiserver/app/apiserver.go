package app

import (
	"context"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/runtime"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/cli/globalflag"
	"k8s.io/component-base/featuregate"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/term"

	"github.com/clusterpedia-io/clusterpedia/cmd/apiserver/app/options"
	"github.com/clusterpedia-io/clusterpedia/pkg/version/verflag"
)

func NewClusterPediaServerCommand(ctx context.Context) *cobra.Command {
	opts := options.NewServerOptions()

	cmd := &cobra.Command{
		Use: "clusterpedia-apiserver",
		RunE: func(cmd *cobra.Command, args []string) error {
			verflag.PrintAndExitIfRequested()
			cliflag.PrintFlags(cmd.Flags())

			if err := opts.Complete(); err != nil {
				return err
			}
			if err := opts.Validate(args); err != nil {
				return err
			}

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

func init() {
	// The feature gate `RemainingItemCount` should default to false
	// https://github.com/clusterpedia-io/clusterpedia/issues/196
	gates := utilfeature.DefaultMutableFeatureGate.GetAll()
	gate := gates[genericfeatures.RemainingItemCount]
	gate.Default = false
	gates[genericfeatures.RemainingItemCount] = gate

	utilfeature.DefaultMutableFeatureGate = featuregate.NewFeatureGate()
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(gates))
	utilfeature.DefaultFeatureGate = utilfeature.DefaultMutableFeatureGate
}
