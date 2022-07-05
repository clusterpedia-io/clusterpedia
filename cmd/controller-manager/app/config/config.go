package config

import (
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	componentbaseconfig "k8s.io/component-base/config"

	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
)

type Config struct {
	Client        *clientset.Clientset
	Kubeconfig    *restclient.Config
	CRDClient     *crdclientset.Clientset
	EventRecorder record.EventRecorder

	LeaderElection componentbaseconfig.LeaderElectionConfiguration
}
