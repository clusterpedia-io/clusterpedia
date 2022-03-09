package filters

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

func TestWithRequestQuery(t *testing.T) {
	tests := []struct {
		name string
		url  string

		expectedQueryFromContext url.Values
	}{
		{
			"url without query",
			"/api",
			url.Values{},
		},
		{
			"url with query",
			"/api?key1=value1&key2=value2",
			url.Values{"key1": []string{"value1"}, "key2": []string{"value2"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var queryFromContext url.Values
			handler := WithRequestQuery(
				http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
					queryFromContext = request.RequestQueryFrom(req.Context())
				}),
			)

			req, _ := http.NewRequest("GET", test.url, nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if !reflect.DeepEqual(queryFromContext, test.expectedQueryFromContext) {
				t.Errorf("expected url query from context: %#v, but got: %#v", test.expectedQueryFromContext, queryFromContext)
			}
		})
	}
}

func TestRemoveFieldSelectorFromRequest(t *testing.T) {
	tests := []struct {
		name string
		url  string

		expectedQueryFromURL     url.Values
		expectedQueryFromContext url.Values
	}{
		{
			"url without query",
			"/api",
			url.Values{},
			url.Values{},
		},
		{
			"url without fieldSelector query",
			"/api?key1=value1&key2=value2",
			url.Values{"key1": []string{"value1"}, "key2": []string{"value2"}},
			url.Values{"key1": []string{"value1"}, "key2": []string{"value2"}},
		},
		{
			"url query with fieldSelector",
			"/api?key1=value1&fieldSelector=value2",
			url.Values{"key1": []string{"value1"}},
			url.Values{"key1": []string{"value1"}, "fieldSelector": []string{"value2"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var queryFromURL, queryFromContext url.Values
			handler := RemoveFieldSelectorFromRequest(
				http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
					queryFromURL = req.URL.Query()
					queryFromContext = request.RequestQueryFrom(req.Context())
				}),
			)

			req, _ := http.NewRequest("GET", test.url, nil)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if _, ok := queryFromURL["fieldSelector"]; ok {
				t.Errorf("expected query from url without 'fieldSelector', but got: %#v", queryFromURL)
			}

			if !reflect.DeepEqual(queryFromContext, test.expectedQueryFromContext) {
				t.Errorf("expected query from context: %#v, but got: %#v", test.expectedQueryFromContext, queryFromContext)
			}

			if !reflect.DeepEqual(queryFromURL, test.expectedQueryFromURL) {
				t.Errorf("expected query from url: %#v, but got: %#v", test.expectedQueryFromURL, queryFromURL)
			}
		})
	}
}
