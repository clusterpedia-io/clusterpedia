package kubeapiserver

import (
	"testing"

	"k8s.io/apimachinery/pkg/labels"
)

func TestTrimForwardLabelForLabelSelectorQuery(t *testing.T) {
	tests := []struct {
		name        string
		selector    string
		wantResult  string
		wantTrimmed bool
	}{
		{
			name:        "empty selector is returned unchanged",
			selector:    "",
			wantResult:  "",
			wantTrimmed: false,
		},
		{
			name:        "selector without forward label is returned unchanged",
			selector:    "foo=bar",
			wantResult:  "foo=bar",
			wantTrimmed: false,
		},
		{
			name:        "forward label alone is trimmed to empty",
			selector:    "search.clusterpedia.io/forward",
			wantResult:  "",
			wantTrimmed: true,
		},
		{
			name:        "forward label with equality is trimmed",
			selector:    "search.clusterpedia.io/forward=true",
			wantResult:  "",
			wantTrimmed: true,
		},
		{
			name:        "forward label leading is trimmed",
			selector:    "search.clusterpedia.io/forward,foo=bar",
			wantResult:  "foo=bar",
			wantTrimmed: true,
		},
		{
			name:        "forward label trailing is trimmed",
			selector:    "foo=bar,search.clusterpedia.io/forward",
			wantResult:  "foo=bar",
			wantTrimmed: true,
		},
		{
			// Regression: previously returned "foo=bar,,baz=qux" which is not a
			// valid label selector.
			name:        "forward label in middle does not introduce empty requirements",
			selector:    "foo=bar,search.clusterpedia.io/forward,baz=qux",
			wantResult:  "baz=qux,foo=bar",
			wantTrimmed: true,
		},
		{
			// Regression: previously returned "a=1,,b=2".
			name:        "forward label with value in middle does not introduce empty requirements",
			selector:    "a=1,search.clusterpedia.io/forward=true,b=2",
			wantResult:  "a=1,b=2",
			wantTrimmed: true,
		},
		{
			// Regression: previously matched as a substring and dropped a
			// different label whose key happens to share the forward prefix.
			name:        "label sharing the forward prefix is preserved",
			selector:    "a=1,search.clusterpedia.io/forward-something=true",
			wantResult:  "a=1,search.clusterpedia.io/forward-something=true",
			wantTrimmed: false,
		},
		{
			// Regression: commas inside set-notation values must not be mistaken
			// for requirement separators.
			name:        "set notation with commas around forward label",
			selector:    "env in (prod,stage),search.clusterpedia.io/forward,tier=frontend",
			wantResult:  "env in (prod,stage),tier=frontend",
			wantTrimmed: true,
		},
		{
			name:        "forward label with exists requirement among others",
			selector:    "a=1,b!=2,search.clusterpedia.io/forward",
			wantResult:  "a=1,b!=2",
			wantTrimmed: true,
		},
		{
			name:        "invalid selector is returned unchanged",
			selector:    ",,,",
			wantResult:  ",,,",
			wantTrimmed: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, trimmed := trimForwardLabelForLabelSelectorQuery(test.selector)
			if trimmed != test.wantTrimmed {
				t.Errorf("expected trimmed=%v, got %v", test.wantTrimmed, trimmed)
			}
			if got != test.wantResult {
				t.Errorf("expected result %q, got %q", test.wantResult, got)
			}

			// The returned selector must always be a valid label selector when
			// the input selector was valid, so that downstream decoders can
			// parse it without a syntax error.
			if _, err := labels.Parse(test.selector); err == nil {
				if _, err := labels.Parse(got); err != nil {
					t.Errorf("returned selector %q is not parseable: %v", got, err)
				}
			}
		})
	}
}
