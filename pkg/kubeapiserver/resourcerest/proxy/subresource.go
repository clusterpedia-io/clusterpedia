package proxy

import (
	"context"
	"net/http"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/registry/rest"
	registryrest "k8s.io/apiserver/pkg/registry/rest"
	api "k8s.io/kubernetes/pkg/apis/core"
)

func GetSubresourceRESTs(connGetter ClusterConnectionGetter) []*PodSubresourceRemoteProxyREST {
	return []*PodSubresourceRemoteProxyREST{
		{
			parent:          schema.GroupResource{Group: "", Resource: "pods"},
			parentKind:      "Pod",
			namespaced:      true,
			subresource:     "attach",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodAttachOptions{},
			connGetter:      connGetter,
		},
		{
			parent:          schema.GroupResource{Group: "", Resource: "pods"},
			parentKind:      "Pod",
			namespaced:      true,
			subresource:     "exec",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodExecOptions{},
			connGetter:      connGetter,
		},
		{
			parent:          schema.GroupResource{Group: "", Resource: "pods"},
			parentKind:      "Pod",
			namespaced:      true,
			subresource:     "portforward",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodPortForwardOptions{},
			connGetter:      connGetter,
		},
		{
			parent:          schema.GroupResource{Group: "", Resource: "pods"},
			parentKind:      "Pod",
			namespaced:      true,
			subresource:     "log",
			methods:         []string{"GET"},
			upgradeRequired: false,
			options:         &api.PodLogOptions{},
			connGetter:      connGetter,
		},
	}
}

type PodSubresourceRemoteProxyREST struct {
	parent     schema.GroupResource
	namespaced bool
	parentKind string

	subresource string

	methods []string
	options runtime.Object

	upgradeRequired bool
	connGetter      ClusterConnectionGetter
}

var _ rest.Storage = &PodSubresourceRemoteProxyREST{}
var _ rest.Connecter = &PodSubresourceRemoteProxyREST{}

func (r *PodSubresourceRemoteProxyREST) ParentGroupResource() schema.GroupResource {
	return r.parent
}

func (r *PodSubresourceRemoteProxyREST) ParentKind() string {
	return r.parentKind
}

func (r *PodSubresourceRemoteProxyREST) Namespaced() bool {
	return r.namespaced
}

func (r *PodSubresourceRemoteProxyREST) Subresource() string {
	return r.subresource
}

func (r *PodSubresourceRemoteProxyREST) New() runtime.Object {
	return r.options.DeepCopyObject()
}

func (r *PodSubresourceRemoteProxyREST) Destroy() {
}

func (r *PodSubresourceRemoteProxyREST) NewConnectOptions() (runtime.Object, bool, string) {
	return r.options.DeepCopyObject(), false, ""
}

func (r *PodSubresourceRemoteProxyREST) ConnectMethods() []string {
	return r.methods
}

func (r *PodSubresourceRemoteProxyREST) Connect(ctx context.Context, name string, opts runtime.Object, responder registryrest.Responder) (http.Handler, error) {
	return proxyConn(ctx, r.connGetter, r.upgradeRequired, proxy.NewErrorResponder(responder), nil)
}
