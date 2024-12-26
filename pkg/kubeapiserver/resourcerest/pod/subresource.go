package pod

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"k8s.io/apimachinery/pkg/runtime"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/proxy"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
	"k8s.io/apiserver/pkg/audit"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	registryrest "k8s.io/apiserver/pkg/registry/rest"
	api "k8s.io/kubernetes/pkg/apis/core"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type ClusterConnectionGetter interface {
	GetClusterDefaultConnection(ctx context.Context, cluster string) (string, http.RoundTripper, error)
	GetClusterConnectionWithTLSConfig(ctx context.Context, cluster string) (string, http.RoundTripper, error)
}

func GetPodSubresourceRESTs(connGetter ClusterConnectionGetter) []*PodSubresourceRemoteProxyREST {
	return []*PodSubresourceRemoteProxyREST{
		{
			subresource:     "attach",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodAttachOptions{},
			connGetter:      connGetter,
		},
		{
			subresource:     "exec",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodExecOptions{},
			connGetter:      connGetter,
		},
		{
			subresource:     "portforward",
			methods:         []string{"GET", "POST"},
			upgradeRequired: true,
			options:         &api.PodPortForwardOptions{},
			connGetter:      connGetter,
		},
		{
			subresource:     "log",
			methods:         []string{"GET"},
			upgradeRequired: false,
			options:         &api.PodLogOptions{},
			connGetter:      connGetter,
		},
	}
}

type PodSubresourceRemoteProxyREST struct {
	subresource     string
	methods         []string
	options         runtime.Object
	upgradeRequired bool

	connGetter ClusterConnectionGetter
}

var _ rest.Storage = &PodSubresourceRemoteProxyREST{}
var _ rest.Connecter = &PodSubresourceRemoteProxyREST{}

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
	clusterName := request.ClusterNameValue(ctx)
	if clusterName == "" {
		return nil, errors.New("missing cluster")
	}

	requestInfo, ok := genericrequest.RequestInfoFrom(ctx)
	if !ok {
		return nil, errors.New("missing RequestInfo")
	}

	// TODO(iceber): need disconnect when the cluster authentication information changes
	endpoint, transport, err := r.connGetter.GetClusterDefaultConnection(ctx, clusterName)
	if err != nil {
		return nil, err
	}

	target, err := url.ParseRequestURI(endpoint + requestInfo.Path)
	if err != nil {
		return nil, err
	}
	target.RawQuery = request.RequestQueryFrom(ctx).Encode()

	proxy := proxy.NewUpgradeAwareHandler(target, transport, false, r.upgradeRequired, proxy.NewErrorResponder(responder))
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		r := req.WithContext(req.Context())
		r.Header = utilnet.CloneHeader(req.Header)
		if auditID, _ := audit.AuditIDFrom(ctx); auditID != "" {
			req.Header.Set(auditinternal.HeaderAuditID, string(auditID))
		}

		proxy.ServeHTTP(rw, req)

		// merge headers
		for _, header := range []string{"Cache-Control", auditinternal.HeaderAuditID} {
			if vs := rw.Header().Values(header); len(vs) > 1 {
				rw.Header().Set(header, vs[0])
			}
		}
	}), nil
}
