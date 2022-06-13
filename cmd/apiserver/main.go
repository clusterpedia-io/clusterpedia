package main

import (
	"fmt"
	"os"

	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/logs"

	"github.com/clusterpedia-io/clusterpedia/cmd/apiserver/app"
)

func main() {
	if err := runApiServerCommand(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runApiServerCommand() error {
	logs.InitLogs()
	defer logs.FlushLogs()

	ctx := apiserver.SetupSignalContext()
	if err := app.NewClusterPediaServerCommand(ctx).Execute(); err != nil {
		return err
	}
	return nil
}
