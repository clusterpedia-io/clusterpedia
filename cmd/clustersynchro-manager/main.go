package main

import (
	"os"

	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register"

	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app"
)

func main() {
	ctx := apiserver.SetupSignalContext()
	command := app.NewClusterSynchroManagerCommand(ctx)
	code := cli.Run(command)
	os.Exit(code)
}
