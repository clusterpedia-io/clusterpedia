package internalstorage

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/runtime/resourceconfig"
	resourceconfigfactory "github.com/clusterpedia-io/clusterpedia/pkg/runtime/resourceconfig/factory"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
)

func testApplyListOptionsToResourceQuery(t *testing.T, name string, options *internal.ListOptions, expected expected) {
	t.Run(fmt.Sprintf("%s postgres", name), func(t *testing.T) {
		postgreSQL, err := toSQL(postgresDB, options,
			func(query *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
				_, _, query, err := applyListOptionsToResourceQuery(postgresDB, query, options)
				return query, err
			},
		)

		assertError(t, expected.err, err)
		if postgreSQL != expected.postgres {
			t.Errorf("expected sql: %q, but got: %q", expected.postgres, postgreSQL)
		}
	})

	for version := range mysqlDBs {
		t.Run(fmt.Sprintf("%s mysql-%s", name, version), func(t *testing.T) {
			mysqlDB := mysqlDBs[version]
			mysqlSQL, err := toSQL(mysqlDB, options,
				func(query *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
					_, _, query, err := applyListOptionsToResourceQuery(mysqlDB, query, options)
					return query, err
				},
			)

			assertError(t, expected.err, err)
			if mysqlSQL != expected.mysql {
				t.Errorf("expected sql: %q, but got: %q", expected.mysql, mysqlSQL)
			}
		})
	}
}

func TestApplyListOptionsToResourceQuery_Owner(t *testing.T) {
	tests := []struct {
		name        string
		listOptions *internal.ListOptions
		expected    expected
	}{
		{
			"owner uid",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1"},
				OwnerUID:     "owner-uid-1",
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND owner_uid = 'owner-uid-1'`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND owner_uid = 'owner-uid-1'",
				"",
			},
		},
		{
			"owner uid with seniority",
			&internal.ListOptions{
				ClusterNames:   []string{"cluster-1"},
				OwnerUID:       "owner-uid-1",
				OwnerSeniority: 1,
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT "uid" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND owner_uid = 'owner-uid-1')`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT `uid` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND owner_uid = 'owner-uid-1')",
				"",
			},
		},
		{
			"owner name",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1"},
				OwnerName:    "owner-name-1",
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT "uid" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND name = 'owner-name-1')`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT `uid` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND name = 'owner-name-1')",
				"",
			},
		},
		{
			"owner uid and name",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1"},
				OwnerUID:     "owner-uid-1",
				OwnerName:    "owner_name-1",
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND owner_uid = 'owner-uid-1'`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND owner_uid = 'owner-uid-1'",
				"",
			},
		},
		{
			"owner name with group resource",
			&internal.ListOptions{
				ClusterNames:       []string{"cluster-1"},
				OwnerName:          "owner-name-1",
				OwnerGroupResource: schema.GroupResource{Group: "apps", Resource: "deployments"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT "uid" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND ("resources"."group" = 'apps' AND "resources"."resource" = 'deployments') AND name = 'owner-name-1')`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND owner_uid IN (SELECT `uid` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND (`resources`.`group` = 'apps' AND `resources`.`resource` = 'deployments') AND name = 'owner-name-1')",
				"",
			},
		},
		{
			"only owner group resource and seniroty",
			&internal.ListOptions{
				ClusterNames:       []string{"cluster-1"},
				OwnerSeniority:     1,
				OwnerGroupResource: schema.GroupResource{Group: "apps", Resource: "deployments"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1'`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1'",
				"",
			},
		},

		// with clusters
		{
			"with multi clusters",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1", "cluster-2"},
				OwnerUID:     "owner-uid-1",
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2')`,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2')",
				"",
			},
		},

		// with namespaces
		{
			"owner uid with namespaces",
			&internal.ListOptions{
				ClusterNames:   []string{"cluster-1"},
				Namespaces:     []string{"ns-1", "ns-2"},
				OwnerUID:       "owner-uid-1",
				OwnerSeniority: 1,
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND namespace IN ('ns-1','ns-2') AND owner_uid IN (SELECT "uid" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND owner_uid = 'owner-uid-1')`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND namespace IN ('ns-1','ns-2') AND owner_uid IN (SELECT `uid` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND owner_uid = 'owner-uid-1')",
				"",
			},
		},
		{
			"owner name with namespaces",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1"},
				Namespaces:   []string{"ns-1", "ns-2"},
				OwnerName:    "owner-name-1",
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' AND namespace IN ('ns-1','ns-2') AND owner_uid IN (SELECT "uid" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND namespace IN ('ns-1','ns-2','') AND name = 'owner-name-1')`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' AND namespace IN ('ns-1','ns-2') AND owner_uid IN (SELECT `uid` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND namespace IN ('ns-1','ns-2','') AND name = 'owner-name-1')",
				"",
			},
		},
	}

	for _, test := range tests {
		testApplyListOptionsToResourceQuery(t, test.name, test.listOptions, test.expected)
	}
}

func TestResourceStorage_genGetObjectQuery(t *testing.T) {
	tests := []struct {
		name         string
		resource     schema.GroupVersionResource
		cluster      string
		namespace    string
		resourceName string
		expected     expected
	}{
		{
			"empty",
			schema.GroupVersionResource{},
			"",
			"",
			"",
			expected{
				`SELECT "object" FROM "resources" WHERE "resources"."cluster" = '' AND "resources"."group" = '' AND "resources"."name" = '' AND "resources"."namespace" = '' AND "resources"."resource" = '' AND "resources"."version" = '' ORDER BY "resources"."id" LIMIT 1`,
				"SELECT `object` FROM `resources` WHERE `resources`.`cluster` = '' AND `resources`.`group` = '' AND `resources`.`name` = '' AND `resources`.`namespace` = '' AND `resources`.`resource` = '' AND `resources`.`version` = '' ORDER BY `resources`.`id` LIMIT 1",
				"",
			},
		},
		{
			"non empty",
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			"cluster-1",
			"ns-1",
			"resource-1",
			expected{
				`SELECT "object" FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND "resources"."group" = 'apps' AND "resources"."name" = 'resource-1' AND "resources"."namespace" = 'ns-1' AND "resources"."resource" = 'deployments' AND "resources"."version" = 'v1' ORDER BY "resources"."id" LIMIT 1`,
				"SELECT `object` FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND `resources`.`group` = 'apps' AND `resources`.`name` = 'resource-1' AND `resources`.`namespace` = 'ns-1' AND `resources`.`resource` = 'deployments' AND `resources`.`version` = 'v1' ORDER BY `resources`.`id` LIMIT 1",
				"",
			},
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s postgres", test.name), func(t *testing.T) {
			postgreSQL := postgresDB.ToSQL(func(tx *gorm.DB) *gorm.DB {
				rs := newTestResourceStorage(tx, test.resource)
				return rs.genGetObjectQuery(context.TODO(), test.cluster, test.namespace, test.resourceName).First(interface{}(nil))
			})

			if postgreSQL != test.expected.postgres {
				t.Errorf("expected sql: %q, but got: %q", test.expected.postgres, postgreSQL)
			}
		})

		for version := range mysqlDBs {
			t.Run(fmt.Sprintf("%s mysql-%s", test.name, version), func(t *testing.T) {
				mysqlSQL := mysqlDBs[version].ToSQL(func(tx *gorm.DB) *gorm.DB {
					rs := newTestResourceStorage(tx, test.resource)
					return rs.genGetObjectQuery(context.TODO(), test.cluster, test.namespace, test.resourceName).First(interface{}(nil))
				})

				if mysqlSQL != test.expected.mysql {
					t.Errorf("expected sql: %q, but got: %q", test.expected.mysql, mysqlSQL)
				}
			})
		}
	}
}

func TestResourceStorage_genListObjectQuery(t *testing.T) {
	tests := []struct {
		name        string
		resource    schema.GroupVersionResource
		listOptions *internal.ListOptions
		expected    expected
	}{
		{
			"empty list options",
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			&internal.ListOptions{},
			expected{
				`SELECT * FROM "resources" WHERE "resources"."group" = 'apps' AND "resources"."resource" = 'deployments' AND "resources"."version" = 'v1'`,
				"SELECT * FROM `resources` WHERE `resources`.`group` = 'apps' AND `resources`.`resource` = 'deployments' AND `resources`.`version` = 'v1'",
				"",
			},
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s postgres", test.name), func(t *testing.T) {
			postgreSQL, err := toSQL(postgresDB.Session(&gorm.Session{DryRun: true}), test.listOptions,
				func(db *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
					rs := newTestResourceStorage(db, test.resource)
					_, _, query, _, err := rs.genListObjectsQuery(context.TODO(), options)
					return query, err
				},
			)

			assertError(t, test.expected.err, err)
			if postgreSQL != test.expected.postgres {
				t.Errorf("expected sql: %q, but got: %q", test.expected.postgres, postgreSQL)
			}
		})

		for version := range mysqlDBs {
			t.Run(fmt.Sprintf("%s mysql-%s", test.name, version), func(t *testing.T) {
				mysqlSQL, err := toSQL(mysqlDBs[version].Session(&gorm.Session{DryRun: true}), test.listOptions,
					func(db *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
						rs := newTestResourceStorage(db, test.resource)
						_, _, query, _, err := rs.genListObjectsQuery(context.TODO(), options)
						return query, err
					},
				)

				assertError(t, test.expected.err, err)
				if mysqlSQL != test.expected.mysql {
					t.Errorf("expected sql: %q, but got: %q", test.expected.mysql, mysqlSQL)
				}
			})
		}
	}
}

func TestResourceStorage_deleteObject(t *testing.T) {
	tests := []struct {
		name         string
		resource     schema.GroupVersionResource
		cluster      string
		namespace    string
		resourceName string
		expected     expected
	}{
		{
			"empty",
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			"",
			"",
			"",
			expected{
				`DELETE FROM "resources" WHERE "resources"."cluster" = '' AND "resources"."group" = 'apps' AND "resources"."name" = '' AND "resources"."namespace" = '' AND "resources"."resource" = 'deployments' AND "resources"."version" = 'v1'`,
				"DELETE FROM `resources` WHERE `resources`.`cluster` = '' AND `resources`.`group` = 'apps' AND `resources`.`name` = '' AND `resources`.`namespace` = '' AND `resources`.`resource` = 'deployments' AND `resources`.`version` = 'v1'",
				"",
			},
		},
		{
			"non empty",
			appsv1.SchemeGroupVersion.WithResource("deployments"),
			"cluster-1",
			"ns-1",
			"resource-1",
			expected{
				`DELETE FROM "resources" WHERE "resources"."cluster" = 'cluster-1' AND "resources"."group" = 'apps' AND "resources"."name" = 'resource-1' AND "resources"."namespace" = 'ns-1' AND "resources"."resource" = 'deployments' AND "resources"."version" = 'v1'`,
				"DELETE FROM `resources` WHERE `resources`.`cluster` = 'cluster-1' AND `resources`.`group` = 'apps' AND `resources`.`name` = 'resource-1' AND `resources`.`namespace` = 'ns-1' AND `resources`.`resource` = 'deployments' AND `resources`.`version` = 'v1'",
				"",
			},
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%s postgres", test.name), func(t *testing.T) {
			// If SkipDefaultTransaction is not set to true, will case
			// 'all expectations were already fulfilled, call to database transaction Begin was not expected'
			postgreSQL := postgresDB.Session(&gorm.Session{SkipDefaultTransaction: true}).ToSQL(
				func(tx *gorm.DB) *gorm.DB {
					rs := newTestResourceStorage(tx, test.resource)
					return rs.deleteObject(test.cluster, test.namespace, test.resourceName)
				})

			if postgreSQL != test.expected.postgres {
				t.Errorf("expected sql: %q, but got: %q", test.expected.postgres, postgreSQL)
			}
		})

		for version := range mysqlDBs {
			t.Run(fmt.Sprintf("%s mysql-%s", test.name, version), func(t *testing.T) {
				// If SkipDefaultTransaction is not set to true, will case
				// 'all expectations were already fulfilled, call to database transaction Begin was not expected'
				mysqlSQL := mysqlDBs[version].Session(&gorm.Session{SkipDefaultTransaction: true}).ToSQL(
					func(tx *gorm.DB) *gorm.DB {
						rs := newTestResourceStorage(tx, test.resource)
						return rs.deleteObject(test.cluster, test.namespace, test.resourceName)
					})

				if mysqlSQL != test.expected.mysql {
					t.Errorf("expected sql: %q, but got: %q", test.expected.mysql, mysqlSQL)
				}
			})
		}
	}
}

func TestResourceStorage_Update(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	db, cleanup, err := newSQLiteDB()
	require.NoError(err)
	require.NotNil(db)
	defer cleanup()

	rs := newTestResourceStorage(db, appsv1.SchemeGroupVersion.WithResource("deployments"))

	factory := resourceconfigfactory.New()
	require.NotNil(factory)

	config, err := factory.NewLegacyResourceConfig(schema.GroupResource{Group: appsv1.SchemeGroupVersion.Group, Resource: "deployments"}, true)
	require.NoError(err)
	require.NotNil(config)

	rs.config = storage.ResourceStorageConfig{ResourceConfig: *config}
	trueRef := true

	obj := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "foobar",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "fooer-id", Name: "fooer", Controller: &trueRef},
			},
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
			},
		},
	}

	metaObj, err := meta.Accessor(obj)
	require.NoError(err)
	ownerRef := metaObj.GetOwnerReferences()
	require.Len(ownerRef, 1)

	var buffer bytes.Buffer
	err = rs.config.Codec.Encode(obj, &buffer)
	require.NoError(err)

	owner := metav1.GetControllerOfNoCopy(metaObj)
	require.NotNil(owner)

	ownerUID := owner.UID
	require.NotEmpty(ownerUID)

	clusterName := "test"

	err = rs.Create(context.Background(), clusterName, obj)
	require.NoError(err)

	var resourcesAfterCreation []Resource
	err = db.
		Where(Resource{
			Cluster:   clusterName,
			Name:      "foo",
			Namespace: "foobar",
		}).
		Find(&resourcesAfterCreation).
		Error
	require.NoError(err)
	require.Len(resourcesAfterCreation, 1)
	assert.NotEmpty(resourcesAfterCreation[0].Object)

	obj.Spec.Template.ObjectMeta.Labels = map[string]string{
		"foo2": "bar2",
	}

	err = rs.Update(context.Background(), clusterName, obj)
	require.NoError(err)

	var resourcesAfterUpdates []Resource
	err = db.
		Where(Resource{
			Cluster:   clusterName,
			Name:      "foo",
			Namespace: "foobar",
		}).
		Find(&resourcesAfterUpdates).
		Error
	require.NoError(err)
	require.Len(resourcesAfterUpdates, 1)
	assert.NotEmpty(resourcesAfterUpdates[0].Object)
	assert.NotEqual(resourcesAfterUpdates[0].Object, resourcesAfterCreation[0].Object)
}

func newTestResourceStorage(db *gorm.DB, storageResource schema.GroupVersionResource) *ResourceStorage {
	return &ResourceStorage{
		db: db,
		config: storage.ResourceStorageConfig{
			ResourceConfig: resourceconfig.ResourceConfig{
				StorageResource: storageResource,
			},
		},
	}
}
