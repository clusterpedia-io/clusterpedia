package utils

import (
	"fmt"
	"testing"

	"k8s.io/client-go/rest"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func kubeconfigWithProxy(proxyURL string) []byte {
	proxyLine := ""
	if proxyURL != "" {
		proxyLine = "    proxy-url: " + proxyURL + "\n"
	}
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: https://apiserver:6443
    insecure-skip-tls-verify: true
%s
contexts:
- name: ctx
  context:
    cluster: c
    user: u
current-context: ctx
users:
- name: u
  user:
    token: tok
`, proxyLine))
}

func buildFromKubeconfig(t *testing.T, kubeconfig []byte) *rest.Config {
	t.Helper()
	cluster := &clusterv1alpha2.PediaCluster{}
	cluster.Spec.Kubeconfig = kubeconfig
	config, err := BuildClusterRestConfig(cluster, nil)
	if err != nil {
		t.Fatalf("build rest config: %v", err)
	}
	return config
}

func TestRestConfigEqual(t *testing.T) {
	withProxy := kubeconfigWithProxy("http://proxy:3128")
	withOtherProxy := kubeconfigWithProxy("http://proxy:3129")
	withoutProxy := kubeconfigWithProxy("")

	tests := []struct {
		name string
		a, b *rest.Config
		want bool
	}{
		{"both nil", nil, nil, true},
		{"one nil", nil, &rest.Config{}, false},
		{"same kubeconfig without proxy", buildFromKubeconfig(t, withoutProxy), buildFromKubeconfig(t, withoutProxy), true},
		{"same kubeconfig with proxy", buildFromKubeconfig(t, withProxy), buildFromKubeconfig(t, withProxy), true},
		{"proxy changed", buildFromKubeconfig(t, withProxy), buildFromKubeconfig(t, withOtherProxy), false},
		{"proxy added", buildFromKubeconfig(t, withoutProxy), buildFromKubeconfig(t, withProxy), false},
		{"token changed", &rest.Config{Host: "h", BearerToken: "a"}, &rest.Config{Host: "h", BearerToken: "b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RestConfigEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("RestConfigEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
