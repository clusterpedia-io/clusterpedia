package clustersynchro

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	clusterv1alpha2 "github.com/clusterpedia-io/api/cluster/v1alpha2"
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
		syncVersions, isLegacyResource, err := negotiateSyncVersions(tc.GroupKind, tc.syncVersions, tc.supportedVersions)
		assert.Equalf(t, tc.wantErr, (err != nil), "testcases[%v] error: %v", i, err)
		assert.Equalf(t, tc.wantSyncVersions, syncVersions, "testcases[%v]", i)
		assert.Equalf(t, tc.wantLegacyResource, isLegacyResource, "testcase[%v]", i)
	}
}

func TestGroupResourceStatus_LoadGroupResourcesStatuses(t *testing.T) {
	status := NewGroupResourceStatus()

	// only add resource
	gr := schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "deployments"}
	status.addResource(gr, "Deployment", true)

	// only add sync condition for resource with version
	gr = schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "configmaps"}
	status.addSyncCondition(gr.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Status: clusterv1alpha2.ResourceSyncStatusPending,
	})

	// add resource and sync condition
	gr = schema.GroupResource{Group: "", Resource: "pods"}
	status.addResource(gr, "Pod", true)
	status.addSyncCondition(gr.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Status: clusterv1alpha2.ResourceSyncStatusPending,
	})

	statuses := status.LoadGroupResourcesStatuses()
	expected := []clusterv1alpha2.ClusterGroupResourcesStatus{
		{
			Group: "apps",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "deployments",
					Kind:       "Deployment",
					Namespaced: true,
				},
			},
		},
		{
			Group: "",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "pods",
					Kind:       "Pod",
					Namespaced: true,
					SyncConditions: []clusterv1alpha2.ClusterResourceSyncCondition{
						{
							Status: clusterv1alpha2.ResourceSyncStatusPending,
						},
					},
				},
			},
		},
	}
	assert.Equal(t, expected, statuses)
}

func TestGroupResourceStatus_UpdateSyncCondition(t *testing.T) {
	status := NewGroupResourceStatus()
	// add resource and sync condition
	gr := schema.GroupResource{Group: "apps", Resource: "deployments"}
	status.addResource(gr, "Deployment", true)
	status.addSyncCondition(gr.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})
	status.addSyncCondition(gr.WithVersion("v1beta1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1beta1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})

	// update the sync condition of added version
	status.UpdateSyncCondition(gr.WithVersion("v1beta1"), clusterv1alpha2.ResourceSyncStatusStop, "", "")

	// update the sync condition of unadded version
	status.UpdateSyncCondition(gr.WithVersion("v1beta2"), clusterv1alpha2.ResourceSyncStatusStop, "", "")

	// update the sync condition of unadded resources
	gr = schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "configmaps"}
	status.UpdateSyncCondition(gr.WithVersion("v1"), clusterv1alpha2.ResourceSyncStatusStop, "", "")

	statuses := status.LoadGroupResourcesStatuses()
	expected := []clusterv1alpha2.ClusterGroupResourcesStatus{
		{
			Group: "apps",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "deployments",
					Kind:       "Deployment",
					Namespaced: true,
					SyncConditions: []clusterv1alpha2.ClusterResourceSyncCondition{
						{
							Version: "v1",
							Status:  clusterv1alpha2.ResourceSyncStatusPending,
						},
						{
							Version: "v1beta1",
							Status:  clusterv1alpha2.ResourceSyncStatusStop,
						},
					},
				},
			},
		},
	}
	assert.Equal(t, expected, statuses)
}

func TestGroupResourceStatus_DeleteVersion(t *testing.T) {
	status := NewGroupResourceStatus()
	// add deployment resource
	deploymentGR := schema.GroupResource{Group: "apps", Resource: "deployments"}
	status.addResource(deploymentGR, "Deployment", true)
	status.addSyncCondition(deploymentGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})
	status.addSyncCondition(deploymentGR.WithVersion("v1beta1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1beta1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})

	// add pod resource
	podGR := schema.GroupResource{Group: "", Resource: "pods"}
	status.addResource(podGR, "Pod", true)
	status.addSyncCondition(podGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Status: clusterv1alpha2.ResourceSyncStatusPending,
	})

	// delete version
	status.DeleteVersion(deploymentGR.WithVersion("v1beta1"))
	status.DeleteVersion(podGR.WithVersion("v1"))

	// delete unadded version
	status.DeleteVersion(deploymentGR.WithVersion("v1alpha1"))

	// delete version of unadded resource
	configmapGR := schema.GroupResource{Group: "", Resource: "configmaps"}
	status.DeleteVersion(configmapGR.WithVersion("v1"))

	statuses := status.LoadGroupResourcesStatuses()
	expected := []clusterv1alpha2.ClusterGroupResourcesStatus{
		{
			Group: "apps",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "deployments",
					Kind:       "Deployment",
					Namespaced: true,
					SyncConditions: []clusterv1alpha2.ClusterResourceSyncCondition{
						{
							Version: "v1",
							Status:  clusterv1alpha2.ResourceSyncStatusPending,
						},
					},
				},
			},
		},
	}
	assert.Equal(t, expected, statuses)
}

func TestGroupResourceStatus_GetStorageGVRToSyncGVRs(t *testing.T) {
	status := NewGroupResourceStatus()
	// add deployment resource
	deploymentGR := schema.GroupResource{Group: "apps", Resource: "deployments"}
	status.addResource(deploymentGR, "Deployment", true)
	status.addSyncCondition(deploymentGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
	})
	status.addSyncCondition(deploymentGR.WithVersion("v1beta1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1beta1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusPending,
	})
	status.addSyncCondition(deploymentGR.WithVersion("v1alpha1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1alpha1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})

	// add pod resource
	podGR := schema.GroupResource{Group: "", Resource: "pods"}
	status.addResource(podGR, "Pod", true)
	status.addSyncCondition(podGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
	})

	gvrSetMap := status.GetStorageGVRToSyncGVRs()
	expected := map[schema.GroupVersionResource]GVRSet{
		deploymentGR.WithVersion("v1"): NewGVRSet(deploymentGR.WithVersion("v1"), deploymentGR.WithVersion("v1beta1")),
		podGR.WithVersion("v1"):        NewGVRSet(podGR.WithVersion("v1")),
	}
	assert.Equal(t, expected, gvrSetMap)
}

func TestGroupResourceStatus_Merge(t *testing.T) {
	status := NewGroupResourceStatus()
	// add deployment resource
	deploymentGR := schema.GroupResource{Group: "apps", Resource: "deployments"}
	status.addResource(deploymentGR, "Deployment", true)
	status.addSyncCondition(deploymentGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
	})
	status.addSyncCondition(deploymentGR.WithVersion("v1beta1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1beta1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusPending,
	})

	// build old GroupResourceStatus
	old := NewGroupResourceStatus()
	// add deployment resource
	old.addResource(deploymentGR, "Deployment", true)
	old.addSyncCondition(deploymentGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})
	old.addSyncCondition(deploymentGR.WithVersion("v1beta2"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version: "v1beta2",
		Status:  clusterv1alpha2.ResourceSyncStatusPending,
	})
	// add pod resource
	podGR := schema.GroupResource{Group: "", Resource: "pods"}
	old.addResource(podGR, "Pod", true)
	old.addSyncCondition(podGR.WithVersion("v1"), clusterv1alpha2.ClusterResourceSyncCondition{
		Version:        "v1",
		StorageVersion: "v1",
		Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
	})

	// merge to `status`
	addition := status.Merge(old)

	statuses := status.LoadGroupResourcesStatuses()
	expected := []clusterv1alpha2.ClusterGroupResourcesStatus{
		{
			Group: "apps",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "deployments",
					Kind:       "Deployment",
					Namespaced: true,
					SyncConditions: []clusterv1alpha2.ClusterResourceSyncCondition{
						{
							Version:        "v1",
							StorageVersion: "v1",
							Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
						},
						{
							Version:        "v1beta1",
							StorageVersion: "v1",
							Status:         clusterv1alpha2.ResourceSyncStatusPending,
						},
						{
							Version: "v1beta2",
							Status:  clusterv1alpha2.ResourceSyncStatusPending,
						},
					},
				},
			},
		},
		{
			Group: "",
			Resources: []clusterv1alpha2.ClusterResourceStatus{
				{
					Name:       "pods",
					Kind:       "Pod",
					Namespaced: true,
					SyncConditions: []clusterv1alpha2.ClusterResourceSyncCondition{
						{
							Version:        "v1",
							StorageVersion: "v1",
							Status:         clusterv1alpha2.ResourceSyncStatusSyncing,
						},
					},
				},
			},
		},
	}
	assert.Equal(t, expected, statuses)

	expectedAddition := NewGVRSet(podGR.WithVersion("v1"), deploymentGR.WithVersion("v1beta2"))
	assert.Equal(t, expectedAddition, addition)
}
