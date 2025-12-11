package filters

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type testCase struct {
	name string
	urls []kubeRequest
}

type kubeRequest struct {
	from    string
	to      string
	rewrite bool
}

var tests = []testCase{
	{
		name: "do rewrite",
		urls: []kubeRequest{
			{
				from:    "/apis/cluster.clusterpedia.io/v1beta1/resourcesany",
				to:      "/apis/clusterpedia.io/v1beta1/resources/apis/cluster.clusterpedia.io/v1beta1/resourcesany",
				rewrite: true,
			},
			{
				from:    "/api/v1/namespaces/default/pods?limit=100",
				to:      "/apis/clusterpedia.io/v1beta1/resources/api/v1/namespaces/default/pods?limit=100",
				rewrite: true,
			},
		},
	},
	{
		name: "none resources paths",
		urls: []kubeRequest{
			{
				from:    "/livez",
				to:      "/livez",
				rewrite: false,
			},
			{
				from:    "/readyz",
				to:      "/readyz",
				rewrite: false,
			},
		},
	},
	{
		name: "not need rewrite",
		urls: []kubeRequest{
			{
				from:    "/apis/clusterpedia.io",
				to:      "/apis/clusterpedia.io",
				rewrite: false,
			},
			{
				from:    "/apis/clusterpedia.io/",
				to:      "/apis/clusterpedia.io/",
				rewrite: false,
			},
			{
				from:    "/apis/clusterpedia.io/any",
				to:      "/apis/clusterpedia.io/any",
				rewrite: false,
			},
			{
				from:    "/apis/clusterpedia.io/v1beta1/resources/api/v1/namespaces/default/pods",
				to:      "/apis/clusterpedia.io/v1beta1/resources/api/v1/namespaces/default/pods",
				rewrite: false,
			},
			{
				from:    "/apis/clusterpedia.io/v1beta1/resources/apis/clusterpedia.io/v1beta1/clusters",
				to:      "/apis/clusterpedia.io/v1beta1/resources/apis/clusterpedia.io/v1beta1/clusters",
				rewrite: false,
			},
		},
	},
	{
		name: "special cases",
		urls: []kubeRequest{
			{
				from:    "/api/v1/namespaces/default/pods?name=abc#xx",
				to:      "/apis/clusterpedia.io/v1beta1/resources/api/v1/namespaces/default/pods?name=abc#xx",
				rewrite: true,
			},
		},
	},
}

func TestUrlPrefixRewrite(t *testing.T) {
	for _, test := range tests {
		t.Logf("Test - name: %s", test.name)

		for _, tmp := range test.urls {
			fromPath, err := url.Parse(tmp.from)
			if err != nil {
				t.Error(err)
			}

			rewritePath, doRewrite := urlPrefixRewrite(fromPath.EscapedPath())
			if doRewrite != tmp.rewrite {
				t.Errorf("Test failed \n from  : %s \n to    : %s \n needRewrite: %v \n doRewrite: %v",
					tmp.from, tmp.to, tmp.rewrite, doRewrite)
			}

			if doRewrite {
				oldURL, err := url.Parse(tmp.to)
				if err != nil {
					t.Error(err)
				}

				if oldURL.EscapedPath() != rewritePath {
					t.Errorf("Test failed \n from  : %s \n to    : %s \n oldPath: %s \n rewritePath: %s",
						tmp.from, tmp.to, oldURL.EscapedPath(), rewritePath)
				}
			}
		}
	}
}

func TestRewrite(t *testing.T) {
	for _, test := range tests {
		t.Logf("Test - name: %s", test.name)

		for _, tmp := range test.urls {
			req, err := http.NewRequest("GET", tmp.from, nil)
			if err != nil {
				t.Fatalf("create HTTP request error: %v", err)
			}

			oldPath := req.URL.EscapedPath()

			h := WithRewriteFilter(
				http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				}),
			)

			t.Logf("From: %s", req.URL.String())

			res := httptest.NewRecorder()
			h.ServeHTTP(res, req)

			t.Logf("Rewrited: %s", req.URL.String())
			if req.URL.String() != tmp.to {
				t.Errorf("Test failed \n from  : %s \n to    : %s \n result: %s",
					tmp.from, tmp.to, req.URL.RequestURI())
			}

			if oldHeaderPath := req.Header.Get(OriginPathHeaderKey); oldHeaderPath != "" {
				if oldPath != oldHeaderPath {
					t.Error("incorrect flag")
				}
			}
		}
	}
}
