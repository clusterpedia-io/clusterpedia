package metrics

import (
	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
	_ "k8s.io/component-base/metrics/prometheus/version"

	"github.com/clusterpedia-io/clusterpedia/pkg/version"
)

var buildInfo = metrics.NewGaugeVec(
	&metrics.GaugeOpts{
		Name: "clusterpedia_build_info",
		Help: "A metric with a constant '1' value labeled by git version, git commit, git tree state, build date, Go version, and compiler from which Clusterpedia was built, and platform on which it is running.",
	},
	[]string{"git_version", "git_commit", "git_tree_state", "build_date", "go_version", "compiler", "platform"},
)

func init() {
	info := version.Get()
	legacyregistry.MustRegister(buildInfo)
	buildInfo.WithLabelValues(info.GitVersion, info.GitCommit, info.GitTreeState, info.BuildDate, info.GoVersion, info.Compiler, info.Platform).Set(1)
}
