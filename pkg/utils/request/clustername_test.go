package request

import (
	"context"
	"testing"
)

func TestClusterNameFrom(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string

		expectedExist                  bool
		expectedClusterNameFromContext string
	}{
		{
			"empty cluster name",
			"",
			false,
			"",
		},
		{
			"non empty cluster name",
			"cluster-1",
			true,
			"cluster-1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := context.TODO()
			ctx := WithClusterName(parent, test.clusterName)
			if test.clusterName == "" && parent != ctx {
				t.Errorf("expected no copy of the parent context with an empty cluster name")
			}

			clusterName, ok := ClusterNameFrom(ctx)
			if ok != test.expectedExist {
				t.Errorf("expected ClusterNameFrom return: %v, but got: %v", test.expectedExist, ok)
			}

			if clusterName != test.expectedClusterNameFromContext {
				t.Errorf("expected cluster name from context: %q, but got: %q", test.clusterName, clusterName)
			}

			if ClusterNameValue(ctx) != test.expectedClusterNameFromContext {
				t.Errorf("expected ClusterNameValue return: %q, but got: %q", test.clusterName, clusterName)
			}
		})
	}
}
