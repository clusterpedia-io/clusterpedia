package resources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	genericrequest "k8s.io/apiserver/pkg/endpoints/request"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

const testResponseTmpl = "Cluster Name: %s, Request Path: %s"

func newResponseBody(cluster string, path string) string {
	return fmt.Sprintf(testResponseTmpl, cluster, path)
}

type testHandler struct{}

func (*testHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	cluster := request.ClusterNameValue(req.Context())
	_, _ = writer.Write([]byte(newResponseBody(cluster, req.URL.Path)))
}

// responder implements rest.Responder for assisting a connector in writing objects or errors.
type responder struct {
	w http.ResponseWriter
}

func (r *responder) Object(statusCode int, obj runtime.Object) {}

func (r *responder) Error(err error) {
	if status, ok := err.(apierrors.APIStatus); ok {
		r.w.WriteHeader(int(status.Status().Code))
	}
	_, _ = r.w.Write([]byte(err.Error()))
}

func newTestRequestInfoResolver() *genericrequest.RequestInfoFactory {
	return &genericrequest.RequestInfoFactory{
		APIPrefixes:          sets.NewString("api", "apis"),
		GrouplessAPIPrefixes: sets.NewString("api"),
	}
}

func TestConnect(t *testing.T) {
	tests := []struct {
		url string

		expectedCode    int
		expectedCluster string
		expectedPath    string
		expectedBody    string
	}{
		{
			"/api/v1/resources",

			200, "", "",
			"",
		},
		{
			"/api/v1/resources/",

			200, "", "/",
			"",
		},
		{
			"/apis/group/v1/resources/",

			200, "", "/",
			"",
		},
		{
			"/apis/group/v1/resources/apis/",

			200, "", "/apis/",
			"",
		},
		{
			"/apis/group/v1/resources/clusters/cluster-1/api?key1=value1&key2=value2",

			200, "cluster-1", "/api", // query will set to req.URL.Query()
			"",
		},
		{
			"/apis/group/v1/resources/clusters/cluster-1/apis/",

			200, "cluster-1", "/apis/",
			"",
		},

		// failed cases
		{
			"/apis/group/v1/resources/clusters/",

			404, "", "",
			"the server could not find the requested resource",
		},
		{
			"/apis/group/v1/resources/clusters",

			404, "", "",
			"the server could not find the requested resource",
		},
		{
			"/apis/group/v1/resources/clusters/cluster-1",

			404, "", "",
			"the server could not find the requested resource",
		},
		{
			"/apis/group/v1/resources/clusters/cluster-1/",

			404, "", "",
			"the server could not find the requested resource",
		},
	}

	rest := NewREST(&testHandler{})
	resolver := newTestRequestInfoResolver()

	for _, test := range tests {
		req, _ := http.NewRequest("GET", test.url, nil)
		apiRequestInfo, err := resolver.NewRequestInfo(req)
		if err != nil {
			t.Fatalf("[%s] NewRequestInfo failed: %s", test.url, err)
		}
		context := genericrequest.WithRequestInfo(context.TODO(), apiRequestInfo)

		writer := httptest.NewRecorder()
		handler, err := rest.Connect(context, apiRequestInfo.Name, nil, &responder{writer})
		if err != nil {
			t.Fatalf("[%s] rest.Connect failed: %s", test.url, err)
		}
		handler.ServeHTTP(writer, req)

		if writer.Code != test.expectedCode {
			t.Errorf("[%s] response code is %d, expected: %d", test.url, writer.Code, test.expectedCode)
		}

		expectedBody := test.expectedBody
		if expectedBody == "" {
			expectedBody = newResponseBody(test.expectedCluster, test.expectedPath)
		}
		if body := writer.Body.String(); body != expectedBody {
			t.Errorf("[%s] response body is %q, expected: %q", test.url, body, expectedBody)
		}
	}
}
