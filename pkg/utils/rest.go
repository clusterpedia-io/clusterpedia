package utils

import (
	"errors"
	"fmt"

	v1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
)

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

func BuildClusterRestConfig(cluster *clusterv1alpha2.PediaCluster, lister v1.SecretNamespaceLister) (*rest.Config, error) {
	// 这是一个简单而粗暴的逻辑，只有在 Spec 没有直接设置任何认证字段时，才会从 Secret 获取认证信息
	// 根据用户的使用接受任何的修改建议
	if len(cluster.Spec.Kubeconfig) == 0 && len(cluster.Spec.TokenData) == 0 &&
		(len(cluster.Spec.CertData) == 0 || len(cluster.Spec.KeyData) == 0) {
		return buildClusterRestConfigFromSecret(cluster.Spec.APIServer, cluster.Spec.CertificationFrom, lister)
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

// buildClusterRestConfigFromSecret 逻辑非常的直接，等待更多的使用建议
func buildClusterRestConfigFromSecret(apiserver string, certification *clusterv1alpha2.ClusterCertification, lister v1.SecretNamespaceLister) (*rest.Config, error) {
	if certification.KubeConfig != nil {
		kubeconfig, err := getValueFromSecret(lister, certification.KubeConfig.Name, certification.KubeConfig.Key)
		if err != nil {
			return nil, fmt.Errorf("Cluster Certification Error: %w", err)
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

	if certification.Token == nil && (certification.Cert == nil || certification.Key == nil) {
		return nil, errors.New("Cluster Certificate Error: token or cert is required")
	}

	config := &rest.Config{
		Host: apiserver,
	}

	if certification.CA != nil {
		caData, err := getValueFromSecret(lister, certification.CA.Name, certification.CA.Key)
		if err != nil {
			return nil, err
		}
		config.TLSClientConfig.CAData = caData
	} else {
		config.TLSClientConfig.Insecure = true
	}

	if certification.CA != nil && certification.Key != nil {
		cert, err := getValueFromSecret(lister, certification.Cert.Name, certification.Cert.Key)
		if err != nil {
			return nil, err
		}
		key, err := getValueFromSecret(lister, certification.Key.Name, certification.Key.Key)
		if err != nil {
			return nil, err
		}
		config.TLSClientConfig.CertData = cert
		config.TLSClientConfig.KeyData = key
	}

	if certification.Token != nil {
		token, err := getValueFromSecret(lister, certification.Token.Name, certification.Token.Key)
		if err != nil {
			return nil, err
		}
		config.BearerToken = string(token)
	}
	return config, nil
}
