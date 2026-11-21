package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/client-go/kubernetes/scheme"
)

func TestRemoteProxyRESTServeHTTPWithoutClusterName(t *testing.T) {
	handler := NewRemoteProxyREST(scheme.Codecs, nil)

	// The request context carries no cluster name, so the proxy cannot pick a cluster.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pods", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}
}
