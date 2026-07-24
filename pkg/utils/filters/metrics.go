package filters

import (
	"net/http"
	"strings"
	"time"

	genericrequest "k8s.io/apiserver/pkg/endpoints/request"

	"github.com/clusterpedia-io/clusterpedia/pkg/metrics"
)

// WithRequestMetrics records request count, duration, response size and
// in-flight gauges for the Clusterpedia APIServer.
//
// resolver builds a low-cardinality handler label from the request path
// (resource type rather than object name). It may be nil.
func WithRequestMetrics(handler http.Handler, resolver genericrequest.RequestInfoResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		name := resolveHandlerLabel(req, resolver)

		metrics.IncInflight(name)
		defer metrics.DecInflight(name)

		rw := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		handler.ServeHTTP(rw, req)

		metrics.RecordRequest(rw.status, req.Method, name, time.Since(start), rw.bytes)
	})
}

func resolveHandlerLabel(req *http.Request, resolver genericrequest.RequestInfoResolver) string {
	if resolver != nil {
		info, err := resolver.NewRequestInfo(req)
		if err == nil && info != nil {
			return handlerLabel(info)
		}
	}
	if info, ok := genericrequest.RequestInfoFrom(req.Context()); ok && info != nil {
		return handlerLabel(info)
	}
	return pathHandler(req.URL.Path)
}

func handlerLabel(info *genericrequest.RequestInfo) string {
	if info.IsResourceRequest {
		return resourceHandler(info)
	}
	if info.Path != "" {
		return info.Path
	}
	return "unknown"
}

func resourceHandler(info *genericrequest.RequestInfo) string {
	// apps/v1/deployments, clusterpedia.io/v1beta1/resources, ...
	var b strings.Builder
	if info.APIGroup != "" {
		b.WriteString(info.APIGroup)
		b.WriteByte('/')
	}
	b.WriteString(info.APIVersion)
	b.WriteByte('/')
	b.WriteString(info.Resource)
	if info.Subresource != "" {
		b.WriteByte('/')
		b.WriteString(info.Subresource)
	}
	return b.String()
}

func pathHandler(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	return strings.TrimRight(path, "/")
}

// responseRecorder captures status and bytes written.
// Unwrap keeps http.ResponseController working; we intentionally do not
// assert Hijacker/Flusher so HTTP/1 vs HTTP/2 capability stays with the
// underlying writer.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.status = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (r *responseRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
