package utils

import (
	"errors"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func BuildClusterRestConfig(cluster *clusterv1alpha2.PediaCluster) (*rest.Config, error) {
	if len(cluster.Spec.Kubeconfig) != 0 {
		clientconfig, err := clientcmd.NewClientConfigFromBytes(cluster.Spec.Kubeconfig)
		if err != nil {
			return nil, err
		}
		return clientconfig.ClientConfig()
	}

	if cluster.Spec.APIServer == "" {
		return nil, errors.New("Cluster APIServer Endpoint is required")
	}

	if len(cluster.Spec.TokenData) == 0 &&
		(len(cluster.Spec.CertData) == 0 || len(cluster.Spec.KeyData) == 0) {
		return nil, errors.New("Cluster APIServer's Token or Cert is required")
	}

	config := &rest.Config{
		Host: cluster.Spec.APIServer,
	}

	if len(cluster.Spec.CAData) != 0 {
		config.TLSClientConfig.CAData = cluster.Spec.CAData
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if len(cluster.Spec.CertData) != 0 && len(cluster.Spec.KeyData) != 0 {
		config.TLSClientConfig.CertData = cluster.Spec.CertData
		config.TLSClientConfig.KeyData = cluster.Spec.KeyData
	}

	if len(cluster.Spec.TokenData) != 0 {
		config.BearerToken = string(cluster.Spec.TokenData)
	}
	return config, nil
}
