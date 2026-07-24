package filters

import (
	"net/http"
	"net/http/httptest"
	"testing"

	genericrequest "k8s.io/apiserver/pkg/endpoints/request"
)

func TestHandlerLabel_ResourceRequest(t *testing.T) {
	tests := []struct {
		name string
		info *genericrequest.RequestInfo
		want string
	}{
		{
			name: "core resource",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: true,
				APIGroup:          "",
				APIVersion:        "v1",
				Resource:          "pods",
			},
			want: "v1/pods",
		},
		{
			name: "named group resource",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: true,
				APIGroup:          "apps",
				APIVersion:        "v1",
				Resource:          "deployments",
			},
			want: "apps/v1/deployments",
		},
		{
			name: "with subresource",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: true,
				APIGroup:          "",
				APIVersion:        "v1",
				Resource:          "pods",
				Subresource:       "status",
			},
			want: "v1/pods/status",
		},
		{
			name: "clusterpedia resources entry",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: true,
				APIGroup:          "clusterpedia.io",
				APIVersion:        "v1beta1",
				Resource:          "resources",
			},
			want: "clusterpedia.io/v1beta1/resources",
		},
		{
			name: "non-resource path",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: false,
				Path:              "/healthz",
			},
			want: "/healthz",
		},
		{
			name: "non-resource empty path",
			info: &genericrequest.RequestInfo{
				IsResourceRequest: false,
			},
			want: "unknown",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := handlerLabel(tc.info); got != tc.want {
				t.Errorf("handlerLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPathHandler(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"", "/"},
		{"/", "/"},
		{"/healthz", "/healthz"},
		{"/metrics/", "/metrics"},
		{"/apis/apps/v1/deployments", "/apis/apps/v1/deployments"},
	}
	for _, tc := range tests {
		if got := pathHandler(tc.path); got != tc.want {
			t.Errorf("pathHandler(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

type fakeResolver struct {
	info *genericrequest.RequestInfo
	err  error
}

func (f fakeResolver) NewRequestInfo(_ *http.Request) (*genericrequest.RequestInfo, error) {
	return f.info, f.err
}

func TestResolveHandlerLabel(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/apis/apps/v1/namespaces/default/deployments/nginx", nil)

	t.Run("uses resolver", func(t *testing.T) {
		got := resolveHandlerLabel(req, fakeResolver{
			info: &genericrequest.RequestInfo{
				IsResourceRequest: true,
				APIGroup:          "apps",
				APIVersion:        "v1",
				Resource:          "deployments",
				Name:              "nginx",
			},
		})
		// object name must not appear in the label
		if got != "apps/v1/deployments" {
			t.Errorf("got %q, want apps/v1/deployments", got)
		}
		if got == req.URL.Path {
			t.Error("handler label should not be the raw path with object name")
		}
	})

	t.Run("falls back to path without resolver", func(t *testing.T) {
		got := resolveHandlerLabel(req, nil)
		if got != "/apis/apps/v1/namespaces/default/deployments/nginx" {
			t.Errorf("got %q", got)
		}
	})
}

func TestWithRequestMetrics_RecordsStatusAndSize(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("hello"))
	})

	h := WithRequestMetrics(inner, fakeResolver{
		info: &genericrequest.RequestInfo{
			IsResourceRequest: true,
			APIGroup:          "apps",
			APIVersion:        "v1",
			Resource:          "deployments",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/apis/apps/v1/namespaces/default/deployments", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != "hello" {
		t.Fatalf("body = %q, want hello", rec.Body.String())
	}
}

func TestResponseRecorder_DefaultStatusOK(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	h := WithRequestMetrics(inner, fakeResolver{
		info: &genericrequest.RequestInfo{IsResourceRequest: false, Path: "/healthz"},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestResponseRecorder_WriteHeaderOnce(t *testing.T) {
	rw := &responseRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	rw.WriteHeader(http.StatusNotFound)
	rw.WriteHeader(http.StatusInternalServerError)
	if rw.status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 after first WriteHeader", rw.status)
	}
}
