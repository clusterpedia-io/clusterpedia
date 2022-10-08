package config

import (
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	componentbaseconfig "k8s.io/component-base/config"

	crdclientset "github.com/clusterpedia-io/clusterpedia/pkg/generated/clientset/versioned"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

type Config struct {
	Kubeconfig    *restclient.Config
	CRDClient     *crdclientset.Clientset
	EventRecorder record.EventRecorder
	BindAddress   string
	SecurePort    int

	StorageFactory storage.StorageFactory

	LeaderElection   componentbaseconfig.LeaderElectionConfiguration
	ClientConnection componentbaseconfig.ClientConnectionConfiguration
}
