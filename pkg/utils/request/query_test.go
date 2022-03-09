package request

import (
	"context"
	"net/url"
	"reflect"
	"testing"
)

func TestRequestQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		nilQuery bool

		expectedHas              bool
		expectedQueryFromContext url.Values
	}{
		{
			"nil url.Values",
			"",
			true,
			false,
			nil,
		},
		{
			"empty url query",
			"",
			false,
			true,
			url.Values{},
		},
		{
			"non empty query",
			"key1=value1&key2=value2&key2=value3",
			false,
			true,
			url.Values{"key1": []string{"value1"}, "key2": []string{"value2", "value3"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			var values url.Values
			if !test.nilQuery {
				if values, err = url.ParseQuery(test.query); err != nil {
					t.Fatalf("url.ParseQuery failed: %v", err)
				}
			}

			parent := context.TODO()
			ctx := WithRequestQuery(parent, values)
			if test.nilQuery && parent != ctx {
				t.Errorf("expected no copy of the parent context with a nil url.Values")
			}

			if HasRequestQuery(ctx) != test.expectedHas {
				t.Errorf("expected HasRequestQuery return %v, but got: %v", test.expectedHas, !test.expectedHas)
			}

			if got := RequestQueryFrom(ctx); !reflect.DeepEqual(got, test.expectedQueryFromContext) {
				t.Errorf("expected RequestQueryFrom return: %#v, but got: %#v", test.expectedQueryFromContext, got)
			}
		})
	}
}
