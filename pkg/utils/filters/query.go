package filters

import (
	"net/http"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

func WithRequestQuery(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(request.WithRequestQuery(req.Context(), req.URL.Query()))
		handler.ServeHTTP(w, req)
	})
}

func RemoveFieldSelectorFromRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !request.HasRequestQuery(req.Context()) {
			req = req.WithContext(request.WithRequestQuery(req.Context(), req.URL.Query()))
		}

		// remove fieldSelector query.
		// 1. prevent `handlers.ListResource` handling and verifying `FieldSelector`
		//     https://github.com/kubernetes/kubernetes/blob/f5be5052e3d0808abb904aebd3218fe4a5c2dd82/staging/src/k8s.io/apiserver/pkg/endpoints/handlers/get.go#L198
		//
		// 2. prevent `RequestInfoFactory.NewRequestInfo` verifying `FieldSelector` on DecodeParameters
		//     https://github.com/kubernetes/kubernetes/blob/f5be5052e3d0808abb904aebd3218fe4a5c2dd82/staging/src/k8s.io/apiserver/pkg/endpoints/request/requestinfo.go#L210-L218
		//
		// issue: https://github.com/clusterpedia-io/clusterpedia/issues/54
		originQuery := req.URL.Query()
		if originQuery.Get("fieldSelector") != "" {
			originQuery.Del("fieldSelector")
			req.URL.RawQuery = originQuery.Encode()
		}

		handler.ServeHTTP(w, req)
	})
}
