module github.com/clusterpedia-io/api

go 1.16

require (
	k8s.io/apimachinery v0.22.4
	k8s.io/code-generator v0.22.2
	sigs.k8s.io/controller-tools v0.7.0
)

replace (
	k8s.io/apimachinery => github.com/k3s-io/kubernetes/staging/src/k8s.io/apimachinery v1.22.4-k3s1
	k8s.io/code-generator => github.com/k3s-io/kubernetes/staging/src/k8s.io/code-generator v1.22.4-k3s1
)
