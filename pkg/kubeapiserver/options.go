package kubeapiserver

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/features"
	proxyrest "github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/resourcerest/proxy"
)

type Options struct {
	// AllowPediaClusterConfigForProxyRequest controls all proxy requests.
	// TODO(iceber): Perhaps we could add a separate setting specifically for subresource proxy request.
	AllowPediaClusterConfigForProxyRequest bool

	AllowedProxySubresources        []string
	ExtraProxyRequestHeaderPrefixes []string

	EnableProxyPathForForwardRequest  bool
	AllowForwardUnsyncResourceRequest bool
}

func NewOptions() *Options {
	return &Options{
		ExtraProxyRequestHeaderPrefixes: []string{proxyrest.DefaultProxyRequestHeaderPrefix},
	}
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	var resources []string
	for r, srs := range supportedProxyCoreSubresources {
		for _, sr := range srs {
			resources = append(resources, r+"/"+sr)
		}
	}

	// To explicitly specify subresources, enabling all subresources of a parent resource
	// using a pattern like `<resource>/*` is currently not supported.
	//
	// If you have a better solution, please submit an issue!
	fs.StringSliceVar(&o.AllowedProxySubresources, "allowed-proxy-subresources", o.AllowedProxySubresources, ""+
		"List of subresources that support proxying requests to the specified cluster, formatted as '[resource/subresource],[subresource],...'. "+
		fmt.Sprintf("Supported proxy subresources include %q.", strings.Join(resources, ",")),
	)

	fs.BoolVar(&o.AllowPediaClusterConfigForProxyRequest, "allow-pediacluster-config-for-proxy-request", o.AllowPediaClusterConfigForProxyRequest, ""+
		"Allow proxy requests to use the cluster configuration from PediaCluster when authentication information cannot be got from the header.",
	)

	fs.BoolVar(&o.EnableProxyPathForForwardRequest, "enable-proxy-path-for-forward-request", o.EnableProxyPathForForwardRequest, ""+
		"Add a '/proxy' path in the API to proxy any request.",
	)
	fs.BoolVar(&o.AllowForwardUnsyncResourceRequest, "allow-forward-unsync-resource-request", o.AllowForwardUnsyncResourceRequest, ""+
		"Allow forwarding requests for unsynchronized resource types."+
		"By default, only requests for resource types configured in PediaCluster can be forwarded.",
	)
}

var supportedProxyCoreSubresources = map[string][]string{
	"pods":     {"proxy", "log", "exec", "attach", "portfowrd"},
	"nodes":    {"proxy"},
	"services": {"proxy"},
}

func (o *Options) Config() (*ExtraConfig, error) {
	if !utilfeature.DefaultFeatureGate.Enabled(features.AllowProxyRequestToClusters) && (len(o.AllowedProxySubresources) != 0 ||
		o.EnableProxyPathForForwardRequest || o.AllowForwardUnsyncResourceRequest) {
		return nil, fmt.Errorf("please enable feature gate %s to allow apiserver to handle the proxy and forward requests", features.AllowProxyRequestToClusters)
	}

	subresources := make(map[schema.GroupResource]sets.Set[string])

	for _, subresource := range o.AllowedProxySubresources {
		var resource string
		switch slice := strings.Split(strings.TrimSpace(subresource), "/"); len(slice) {
		case 1:
			subresource = slice[0]
		case 2:
			resource, subresource = slice[0], slice[1]
		default:
			return nil, fmt.Errorf("--allowed-proxy-subresources: invalid format %q", subresource)
		}

		var matched bool
		for r, srs := range supportedProxyCoreSubresources {
			for _, sr := range srs {
				if (resource == "" || resource == r) && subresource == sr {
					gr := schema.GroupResource{Group: "", Resource: r}
					set := subresources[gr]
					if set == nil {
						set = sets.New[string]()
						subresources[gr] = set
					}
					set.Insert(sr)
					matched = true
					break
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("--allowed-proxy-subresources: unsupported subresources or invalid format %q", subresource)
		}
	}
	return &ExtraConfig{
		AllowPediaClusterConfigReuse:      o.AllowPediaClusterConfigForProxyRequest,
		AllowedProxySubresources:          subresources,
		EnableProxyPathForForwardRequest:  o.EnableProxyPathForForwardRequest,
		AllowForwardUnsyncResourceRequest: o.AllowForwardUnsyncResourceRequest,
	}, nil
}
