package kubeapiserver

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseAllowedProxySubresources(t *testing.T) {
	tests := []struct {
		name        string
		allowed     []string
		wantError   bool
		wantErrText string
		wantHasPods []string
	}{
		{
			name:        "canonical portforward",
			allowed:     []string{"pods/portforward"},
			wantHasPods: []string{"portforward"},
		},
		{
			name:        "legacy typo alias",
			allowed:     []string{"pods/portfowrd"},
			wantHasPods: []string{"portforward"},
		},
		{
			name:        "resource omitted",
			allowed:     []string{"portforward"},
			wantHasPods: []string{"portforward"},
		},
		{
			name:        "unsupported subresource",
			allowed:     []string{"pods/trace"},
			wantError:   true,
			wantErrText: `"pods/trace"`,
		},
		{
			name:        "unsupported resource keeps resource in error",
			allowed:     []string{"foo/portforward"},
			wantError:   true,
			wantErrText: `"foo/portforward"`,
		},
		{
			name:        "invalid format keeps original token in error",
			allowed:     []string{"pods/log/extra"},
			wantError:   true,
			wantErrText: `"pods/log/extra"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseAllowedProxySubresources(test.allowed)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if test.wantErrText != "" && !strings.Contains(err.Error(), test.wantErrText) {
					t.Fatalf("expected error to contain %s, got %q", test.wantErrText, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			pods := got[schema.GroupResource{Resource: "pods"}]
			if pods == nil {
				t.Fatalf("expected pods subresources to be present")
			}
			for _, want := range test.wantHasPods {
				if !pods.Has(want) {
					t.Fatalf("expected pods subresources to contain %q, got %v", want, pods.UnsortedList())
				}
			}
		})
	}
}
