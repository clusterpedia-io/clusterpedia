package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"k8s.io/client-go/rest"

	clusterlister "github.com/clusterpedia-io/clusterpedia/pkg/generated/listers/cluster/v1alpha2"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
)

const DefaultProxyRequestHeaderPrefix = "X-Clusterpedia-Proxy-"

type Connector struct {
	allowConfigReuse    bool
	extraHeaderPrefixes []string
	clusterLister       clusterlister.PediaClusterLister
}

func NewProxyConnector(clusterLister clusterlister.PediaClusterLister, allowPediaClusterConfigReuse bool, extraHeaderPrefixes []string) ClusterConnectionGetter {
	if len(extraHeaderPrefixes) == 0 {
		extraHeaderPrefixes = []string{DefaultProxyRequestHeaderPrefix}
	}
	return &Connector{
		allowConfigReuse:    allowPediaClusterConfigReuse,
		extraHeaderPrefixes: extraHeaderPrefixes,
		clusterLister:       clusterLister,
	}
}

func (c *Connector) GetClusterConnection(ctx context.Context, name string, req *http.Request) (string, http.RoundTripper, error) {
	cluster, err := c.clusterLister.Get(name)
	if err != nil {
		return "", nil, err
	}
	config := &rest.Config{}
	if cluster.Status.APIServer != "" {
		config.Host = cluster.Status.APIServer
	} else if cluster.Spec.APIServer != "" {
		config.Host = cluster.Spec.APIServer
	} else {
		return "", nil, errors.New(".Spec.APIServer and .Status.APIServer are empty")
	}
	config.TLSClientConfig.Insecure = true

	var authInHeader bool
	extra := map[string][]string{}

	headers := req.Header.Clone()
	for _, prefix := range c.extraHeaderPrefixes {
		for headerName, vv := range headers {
			if !(len(headerName) >= len(prefix) && strings.EqualFold(headerName[:len(prefix)], prefix)) {
				continue
			}

			extraKey := unescapeExtraKey(strings.ToLower(headerName[len(prefix):]))
			extra[extraKey] = append(extra[extraKey], vv...)
			req.Header.Del(headerName)
		}
	}

	for key, vals := range extra {
		switch key {
		case "ca":
			authInHeader = true
			if len(vals) > 0 {
				config.TLSClientConfig.CAData, err = base64.StdEncoding.DecodeString(vals[0])
				if err != nil {
					return "", nil, err
				}
				config.TLSClientConfig.Insecure = false
			}
		case "token":
			authInHeader = true
			if len(vals) > 0 {
				config.BearerToken = vals[0]
			}
		case "client-cert":
			authInHeader = true
			if len(vals) > 0 {
				config.TLSClientConfig.CertData, err = base64.StdEncoding.DecodeString(vals[0])
				if err != nil {
					return "", nil, err
				}
			}
		case "client-key":
			authInHeader = true
			if len(vals) > 0 {
				config.TLSClientConfig.KeyData, err = base64.StdEncoding.DecodeString(vals[0])
				if err != nil {
					return "", nil, err
				}
			}
		}
	}

	if !authInHeader && c.allowConfigReuse {
		config, err = utils.BuildClusterRestConfig(cluster)
		if err != nil {
			return "", nil, err
		}
	}

	transport, err := rest.TransportFor(config)
	if err != nil {
		return "", nil, err
	}
	return config.Host, transport, nil
}

func unescapeExtraKey(encodedKey string) string {
	key, err := url.PathUnescape(encodedKey) // Decode %-encoded bytes.
	if err != nil {
		return encodedKey // Always record extra strings, even if malformed/unencoded.
	}
	return key
}
