package v1beta1

import (
	"strings"
	"testing"

	"github.com/clusterpedia-io/api/clusterpedia"
)

func TestConvertListOptionsRejectsNegativeOwnerSeniority(t *testing.T) {
	in := &ListOptions{OwnerSeniority: -1}
	out := &clusterpedia.ListOptions{}

	err := Convert_v1beta1_ListOptions_To_clusterpedia_ListOptions(in, out, nil)
	if err == nil {
		t.Fatal("expected negative ownerSeniority to be rejected")
	}
	if !strings.Contains(err.Error(), "OwnerSeniority(-1)") {
		t.Fatalf("expected error to include ownerSeniority value, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("expected non-negative validation error, got %q", err.Error())
	}
}

func TestConvertListOptionsAcceptsNonNegativeOwnerSeniority(t *testing.T) {
	in := &ListOptions{OwnerSeniority: 1}
	out := &clusterpedia.ListOptions{}

	if err := Convert_v1beta1_ListOptions_To_clusterpedia_ListOptions(in, out, nil); err != nil {
		t.Fatalf("expected non-negative ownerSeniority to be accepted, got %v", err)
	}
	if out.OwnerSeniority != 1 {
		t.Fatalf("expected ownerSeniority to be converted, got %d", out.OwnerSeniority)
	}
}
