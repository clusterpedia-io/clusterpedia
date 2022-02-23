package version

import (
	"fmt"
	"runtime"
	"strings"

	apimachineryversion "k8s.io/apimachinery/pkg/version"
	componentbaseversion "k8s.io/component-base/version"
)

type Info struct {
	GitVersion   string `json:"gitVersion"`
	GitCommit    string `json:"gitCommit"`
	GitTreeState string `json:"gitTreeState"`
	BuildDate    string `json:"buildDate"`
	GoVersion    string `json:"goVersion"`
	Compiler     string `json:"compiler"`
	Platform     string `json:"platform"`
	KubeVersion  string `json:"kubeVersion"`
}

// String returns info as a human-friendly version string.
func (info Info) String() string {
	return info.GitVersion
}

func Get() Info {
	return Info{
		GitVersion:   gitVersion,
		GitCommit:    gitCommit,
		GitTreeState: gitTreeState,
		BuildDate:    buildDate,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Platform:     fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		KubeVersion:  GetKubeVersion().String(),
	}
}

func GetKubeVersion() apimachineryversion.Info {
	// kubernetes repo is use https://github.com/k3s-io/kubernetes
	// k3s-io/kubernetes/staging/src/k8s.io/component-base/version.gitVersion is "v0.0.0-k3s1"
	// remove suffix '-k3s1'
	version := componentbaseversion.Get()
	version.GitVersion = strings.TrimSuffix(version.GitVersion, "-k3s1")
	return version
}
