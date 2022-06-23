package main

import (
	"os"

	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register" // for JSON log format registration

	"github.com/clusterpedia-io/clusterpedia/cmd/apiserver/app"
)

func main() {
	ctx := apiserver.SetupSignalContext()
	command := app.NewClusterPediaServerCommand(ctx)
	code := cli.Run(command)
	os.Exit(code)
}
