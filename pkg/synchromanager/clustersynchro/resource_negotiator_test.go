package clustersynchro

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNegotiateSyncVersions(t *testing.T) {
	testcases := []struct {
		GroupKind schema.GroupKind

		syncVersions      []string
		supportedVersions []string

		wantSyncVersions   []string
		wantLegacyResource bool
		wantErr            bool
	}{
		// Legacy Resource
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{"v1"},
			supportedVersions: []string{"v1"},

			wantSyncVersions:   []string{"v1"},
			wantLegacyResource: true,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{"v1beta1"},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1"},

			wantSyncVersions:   []string{"v1"},
			wantLegacyResource: true,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{"v1alpha1"},
			supportedVersions: []string{"v1beta2", "v1beta1"},

			wantSyncVersions:   []string{"v1beta2"},
			wantLegacyResource: true,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{},
			supportedVersions: []string{"v2", "v1", "v1beta2"},

			wantSyncVersions:   []string{"v1"},
			wantLegacyResource: true,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{},
			supportedVersions: []string{"v2", "v1beta2"},

			wantSyncVersions:   []string{"v1beta2"},
			wantLegacyResource: true,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{},
			supportedVersions: []string{"v2"},

			wantSyncVersions:   nil,
			wantLegacyResource: true,
			wantErr:            true,
		},
		{
			GroupKind: schema.GroupKind{Group: "apps", Kind: "Deployment"},

			syncVersions:      []string{},
			supportedVersions: []string{"v2", "v1alpha2"},

			wantSyncVersions:   nil,
			wantLegacyResource: true,
			wantErr:            true,
		},

		// Custom Resource that do not set sync versions
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{},
			supportedVersions: []string{"v1beta2", "v1beta1"},

			wantSyncVersions:   []string{"v1beta2", "v1beta1"},
			wantLegacyResource: false,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1", "v1alpha2", "v1alpha1"},

			wantSyncVersions:   []string{"v1", "v1beta2", "v1beta1"},
			wantLegacyResource: false,
			wantErr:            false,
		},

		// Custom Resource that specify sync versions
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{"v1beta2"},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1", "v1alpha2", "v1alpha1"},

			wantSyncVersions:   []string{"v1beta2"},
			wantLegacyResource: false,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{"v1beta2", "v1alpha2"},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1", "v1alpha2", "v1alpha1"},

			wantSyncVersions:   []string{"v1beta2", "v1alpha2"},
			wantLegacyResource: false,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{"v2beta1", "v1alpha2"},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1", "v1alpha2", "v1alpha1"},

			wantSyncVersions:   []string{"v1alpha2"},
			wantLegacyResource: false,
			wantErr:            false,
		},
		{
			GroupKind: schema.GroupKind{Group: "test.io", Kind: "CustomKind"},

			syncVersions:      []string{"v2beta1"},
			supportedVersions: []string{"v1", "v1beta2", "v1beta1", "v1alpha2", "v1alpha1"},

			wantSyncVersions:   nil,
			wantLegacyResource: false,
			wantErr:            true,
		},
	}

	for i, tc := range testcases {
		gvks := make([]schema.GroupVersionKind, 0, len(tc.supportedVersions))
		for _, v := range tc.supportedVersions {
			gvks = append(gvks, tc.GroupKind.WithVersion(v))
		}

		syncVersions, isLegacyResource, err := negotiateSyncVersions(tc.syncVersions, gvks)
		assert.Equalf(t, tc.wantErr, (err != nil), "testcases[%v] error: %v", i, err)
		assert.Equalf(t, tc.wantSyncVersions, syncVersions, "testcases[%v]", i)
		assert.Equalf(t, tc.wantLegacyResource, isLegacyResource, "testcase[%v]", i)
	}
}
