package utils

import (
	"errors"
	"fmt"

	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

func BuildClusterRestConfig(cluster *clusterv1alpha2.PediaCluster, lister v1.SecretNamespaceLister) (*rest.Config, error) {
	// This is a simple and straightforward logic: authentication information is only retrieved
	// from the Secret if no authentication fields are directly set in the Spec.
	//
	// Open to any modification suggestions based on user feedback.
	if len(cluster.Spec.Kubeconfig) == 0 && len(cluster.Spec.TokenData) == 0 &&
		(len(cluster.Spec.CertData) == 0 || len(cluster.Spec.KeyData) == 0) &&
		cluster.Spec.AuthenticationFrom != nil {
		if lister == nil {
			return nil, fmt.Errorf("cluster authentication secret listers is nil, perhaps you need to enable feature gate %s", "ClusterAuthenticationFromSecret")
		}
		config, err := buildClusterRestConfigFromSecret(cluster.Spec.APIServer, cluster.Spec.AuthenticationFrom, lister)
		if err != nil {
			return nil, fmt.Errorf("Cluster Authentication Error: %w", err)
		}
		return config, nil
	}

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

// The logic is very direct. Awaiting more usage suggestions.
func buildClusterRestConfigFromSecret(apiserver string, auth *clusterv1alpha2.ClusterAuthentication, lister v1.SecretNamespaceLister) (*rest.Config, error) {
	if auth.KubeConfig != nil {
		kubeconfig, err := getValueFromSecret(lister, auth.KubeConfig.Name, auth.KubeConfig.Key)
		if err != nil {
			return nil, err
		}
		clientconfig, err := clientcmd.NewClientConfigFromBytes(kubeconfig)
		if err != nil {
			return nil, err
		}
		return clientconfig.ClientConfig()
	}

	if apiserver == "" {
		return nil, errors.New("Cluster APIServer Endpoint is required")
	}

	if auth.Token == nil && (auth.Cert == nil || auth.Key == nil) {
		return nil, errors.New("token or cert is required")
	}

	config := &rest.Config{
		Host: apiserver,
	}

	if auth.CA != nil {
		caData, err := getValueFromSecret(lister, auth.CA.Name, auth.CA.Key)
		if err != nil {
			return nil, err
		}
		config.TLSClientConfig.CAData = caData
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if auth.Cert != nil && auth.Key != nil {
		cert, err := getValueFromSecret(lister, auth.Cert.Name, auth.Cert.Key)
		if err != nil {
			return nil, err
		}
		key, err := getValueFromSecret(lister, auth.Key.Name, auth.Key.Key)
		if err != nil {
			return nil, err
		}
		config.TLSClientConfig.CertData = cert
		config.TLSClientConfig.KeyData = key
	}

	if auth.Token != nil {
		token, err := getValueFromSecret(lister, auth.Token.Name, auth.Token.Key)
		if err != nil {
			return nil, err
		}
		config.BearerToken = string(token)
	}
	return config, nil
}

func getValueFromSecret(lister v1.SecretNamespaceLister, name string, key string) ([]byte, error) {
	secret, err := lister.Get(name)
	if err != nil {
		return nil, err
	}
	value := secret.Data[key]
	if len(value) == 0 {
		return nil, fmt.Errorf("secret %s's %s is empty", name, key)
	}
	return value, nil
}
