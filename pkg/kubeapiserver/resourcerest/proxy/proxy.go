package proxy

import (
	"context"
	"net/http"
	"net/url"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/proxy"
	auditinternal "k8s.io/apiserver/pkg/apis/audit"
	"k8s.io/apiserver/pkg/audit"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type ClusterConnectionGetter interface {
	GetClusterConnection(ctx context.Context, cluster string, req *http.Request) (string, http.RoundTripper, error)
}

type RemoteProxyREST struct {
	serializer runtime.NegotiatedSerializer
	connGetter ClusterConnectionGetter
}

func NewRemoteProxyREST(serializer runtime.NegotiatedSerializer, connGetter ClusterConnectionGetter) http.Handler {
	return &RemoteProxyREST{serializer: serializer, connGetter: connGetter}
}

func (r *RemoteProxyREST) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	handler, err := proxyConn(req.Context(), r.connGetter, false, r, nil)
	if err != nil {
		r.Error(rw, req, err)
	}
	handler.ServeHTTP(rw, req)
}

func (r *RemoteProxyREST) Error(w http.ResponseWriter, req *http.Request, err error) {
	responsewriters.ErrorNegotiated(err, r.serializer, schema.GroupVersion{}, w, req)
}

func proxyConn(ctx context.Context, connGetter ClusterConnectionGetter, upgradeRequired bool, responder proxy.ErrorResponder, wrapProxy func(*proxy.UpgradeAwareHandler) http.Handler) (http.Handler, error) {
	clusterName := request.ClusterNameValue(ctx)
	if clusterName == "" {
		return nil, apierrors.NewBadRequest("please specify the cluster name when using proxy model.")
	}

	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// TODO(iceber): need disconnect when the cluster authentication information changes
		endpoint, transport, err := connGetter.GetClusterConnection(ctx, clusterName, req)
		if err != nil {
			responder.Error(rw, req, err)
			return
		}

		target, err := url.ParseRequestURI(endpoint + req.URL.Path)
		if err != nil {
			responder.Error(rw, req, err)
			return
		}

		proxy := proxy.NewUpgradeAwareHandler(target, transport, false, upgradeRequired, responder)
		proxy.UseLocationHost = true

		var handler http.Handler = proxy
		if wrapProxy != nil {
			handler = wrapProxy(proxy)
		}

		r := req.Clone(req.Context())
		if auditID, _ := audit.AuditIDFrom(ctx); auditID != "" {
			r.Header.Set(auditinternal.HeaderAuditID, string(auditID))
		}
		// get URL Query from context.Context
		if query := request.RequestQueryFrom(ctx); query != nil {
			r.URL.RawQuery = query.Encode()
		}

		handler.ServeHTTP(rw, r)

		// merge headers
		for _, header := range []string{"Cache-Control", auditinternal.HeaderAuditID} {
			if vs := rw.Header().Values(header); len(vs) > 1 {
				rw.Header().Set(header, vs[0])
			}
		}
	}), nil
}
