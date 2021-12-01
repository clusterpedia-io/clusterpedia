package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/logs"

	"github.com/clusterpedia-io/clusterpedia/cmd/clustersynchro-manager/app"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	logs.InitLogs()
	defer logs.FlushLogs()

	ctx := apiserver.SetupSignalContext()
	if err := app.NewClusterSynchroManagerCommand(ctx).Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
