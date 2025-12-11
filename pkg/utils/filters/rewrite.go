package filters

import (
	"net/http"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

const OriginPathHeaderKey = "X-Rewrite-Original-Path"
const ApiServicePrefix = "/apis/clusterpedia.io"
const OldResourceApiServerPrefixWithoutSlash = "/apis/clusterpedia.io/v1beta1/resources"
const OldResourceApiServerPrefix = OldResourceApiServerPrefixWithoutSlash + "/"

var ExcludePrefixPaths = sets.NewString("", "/livez", "/readyz", OldResourceApiServerPrefixWithoutSlash)

func WithRewriteFilter(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		oldPath := req.URL.EscapedPath()
		if rewritePath, ok := urlPrefixRewrite(oldPath); ok {
			req.URL.Path = rewritePath
			req.Header.Set(OriginPathHeaderKey, oldPath)
			klog.V(5).InfoS("request need rewrite", "oldPath", oldPath, "newPath", req.URL.EscapedPath())
		}
		handler.ServeHTTP(w, req)
	})
}

func urlPrefixRewrite(oldPath string) (string, bool) {
	if strings.HasPrefix(oldPath, ApiServicePrefix) || ExcludePrefixPaths.Has(oldPath) {
		return "", false
	}

	rewritePath, err := url.JoinPath(OldResourceApiServerPrefix, oldPath)
	if err != nil {
		return "", false
	}
	return rewritePath, true
}
