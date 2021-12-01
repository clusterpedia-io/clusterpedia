module github.com/clusterpedia-io/clusterpedia

go 1.16

require (
	github.com/go-sql-driver/mysql v1.6.0
	github.com/jackc/pgconn v1.10.0
	github.com/jackc/pgerrcode v0.0.0-20201024163028-a0d42d470451
	github.com/jackc/pgx/v4 v4.13.0
	github.com/jinzhu/configor v1.2.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	gorm.io/datatypes v1.0.3
	gorm.io/driver/mysql v1.2.0
	gorm.io/driver/postgres v1.2.2
	gorm.io/gorm v1.22.3
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.4
	k8s.io/apiserver v0.0.0
	k8s.io/client-go v0.22.4
	k8s.io/code-generator v0.0.0
	k8s.io/component-base v0.22.4
	k8s.io/klog/v2 v2.9.0
	k8s.io/kubernetes v1.22.4
	sigs.k8s.io/controller-runtime v0.10.3
	sigs.k8s.io/controller-tools v0.7.0
)

replace (
	k8s.io/api => github.com/k3s-io/kubernetes/staging/src/k8s.io/api v1.22.4-k3s1
	k8s.io/apiextensions-apiserver => github.com/k3s-io/kubernetes/staging/src/k8s.io/apiextensions-apiserver v1.22.4-k3s1
	k8s.io/apimachinery => github.com/k3s-io/kubernetes/staging/src/k8s.io/apimachinery v1.22.4-k3s1
	k8s.io/apiserver => github.com/k3s-io/kubernetes/staging/src/k8s.io/apiserver v1.22.4-k3s1
	k8s.io/cli-runtime => github.com/k3s-io/kubernetes/staging/src/k8s.io/cli-runtime v1.22.4-k3s1
	k8s.io/client-go => github.com/k3s-io/kubernetes/staging/src/k8s.io/client-go v1.22.4-k3s1
	k8s.io/cloud-provider => github.com/k3s-io/kubernetes/staging/src/k8s.io/cloud-provider v1.22.4-k3s1
	k8s.io/cluster-bootstrap => github.com/k3s-io/kubernetes/staging/src/k8s.io/cluster-bootstrap v1.22.4-k3s1
	k8s.io/code-generator => github.com/k3s-io/kubernetes/staging/src/k8s.io/code-generator v1.22.4-k3s1
	k8s.io/component-base => github.com/k3s-io/kubernetes/staging/src/k8s.io/component-base v1.22.4-k3s1
	k8s.io/component-helpers => github.com/k3s-io/kubernetes/staging/src/k8s.io/component-helpers v1.22.4-k3s1
	k8s.io/controller-manager => github.com/k3s-io/kubernetes/staging/src/k8s.io/controller-manager v1.22.4-k3s1
	k8s.io/cri-api => github.com/k3s-io/kubernetes/staging/src/k8s.io/cri-api v1.22.4-k3s1
	k8s.io/csi-translation-lib => github.com/k3s-io/kubernetes/staging/src/k8s.io/csi-translation-lib v1.22.4-k3s1
	k8s.io/kube-aggregator => github.com/k3s-io/kubernetes/staging/src/k8s.io/kube-aggregator v1.22.4-k3s1
	k8s.io/kube-controller-manager => github.com/k3s-io/kubernetes/staging/src/k8s.io/kube-controller-manager v1.22.4-k3s1
	k8s.io/kube-proxy => github.com/k3s-io/kubernetes/staging/src/k8s.io/kube-proxy v1.22.4-k3s1
	k8s.io/kube-scheduler => github.com/k3s-io/kubernetes/staging/src/k8s.io/kube-scheduler v1.22.4-k3s1
	k8s.io/kubectl => github.com/k3s-io/kubernetes/staging/src/k8s.io/kubectl v1.22.4-k3s1
	k8s.io/kubelet => github.com/k3s-io/kubernetes/staging/src/k8s.io/kubelet v1.22.4-k3s1
	k8s.io/kubernetes => github.com/k3s-io/kubernetes v1.22.4-k3s1
	k8s.io/legacy-cloud-providers => github.com/k3s-io/kubernetes/staging/src/k8s.io/legacy-cloud-providers v1.22.4-k3s1
	k8s.io/metrics => github.com/k3s-io/kubernetes/staging/src/k8s.io/metrics v1.22.4-k3s1
	k8s.io/mount-utils => github.com/k3s-io/kubernetes/staging/src/k8s.io/mount-utils v1.22.4-k3s1
	k8s.io/pod-security-admission => github.com/k3s-io/kubernetes/staging/src/k8s.io/pod-security-admission v1.22.4-k3s1
	k8s.io/sample-apiserver => github.com/k3s-io/kubernetes/staging/src/k8s.io/sample-apiserver v1.22.4-k3s1
	k8s.io/sample-cli-plugin => github.com/k3s-io/kubernetes/staging/src/k8s.io/sample-cli-plugin v1.22.4-k3s1
	k8s.io/sample-controller => github.com/k3s-io/kubernetes/staging/src/k8s.io/sample-controller v1.22.4-k3s1
)
