package kubeapiserver

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Options struct {
	AllowedProxySubresources []string
}

func NewOptions() *Options {
	return &Options{}
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
		fmt.Sprintf("Supported proxy subresources include %q", strings.Join(resources, ",")),
	)
}

var supportedProxyCoreSubresources = map[string][]string{
	"pods":     {"proxy", "log", "exec", "attach", "portfowrd"},
	"nodes":    {"proxy"},
	"services": {"proxy"},
}

func (o *Options) Config() (*ExtraConfig, error) {
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
	return &ExtraConfig{AllowedProxySubresources: subresources}, nil
}
