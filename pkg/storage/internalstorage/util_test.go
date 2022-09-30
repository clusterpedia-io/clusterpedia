package internalstorage

import (
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefields "k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/api/clusterpedia/fields"
)

type expected struct {
	postgres string
	mysql    string
	err      string
}

func testApplyListOptionsToQuery(t *testing.T, name string, options *internal.ListOptions, expected expected) {
	t.Run(fmt.Sprintf("%s postgres", name), func(t *testing.T) {
		postgreSQL, err := toSQL(postgresDB, options,
			func(query *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
				_, _, query, err := applyListOptionsToQuery(query, options, nil)
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
			mysqlSQL, err := toSQL(mysqlDBs[version], options,
				func(query *gorm.DB, options *internal.ListOptions) (*gorm.DB, error) {
					_, _, query, err := applyListOptionsToQuery(query, options, nil)
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

func TestApplyListOptionsToQuery_Metadata(t *testing.T) {
	since, _ := time.Parse("2006-01-02", "2022-03-04")
	before, _ := time.Parse("2006-01-02", "2022-03-15")

	tests := []struct {
		name        string
		listOptions *internal.ListOptions
		expected    expected
	}{
		{
			"with cluster",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1' `,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1' ",
				"",
			},
		},
		{
			"with clusters",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1", "cluster-2"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2') `,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2') ",
				"",
			},
		},
		{
			"with clusters and namespace",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1", "cluster-2"},
				Namespaces:   []string{"ns-1"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2') AND namespace = 'ns-1' `,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2') AND namespace = 'ns-1' ",
				"",
			},
		},
		{
			"with clusters and namespaces",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1", "cluster-2"},
				Namespaces:   []string{"ns-1", "ns-2"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2') AND namespace IN ('ns-1','ns-2') `,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2') AND namespace IN ('ns-1','ns-2') ",
				"",
			},
		},
		{
			"with namespace and name",
			&internal.ListOptions{
				Namespaces: []string{"ns-1"},
				Names:      []string{"name-1"},
			},
			expected{
				`SELECT * FROM "resources" WHERE namespace = 'ns-1' AND name = 'name-1' `,
				"SELECT * FROM `resources` WHERE namespace = 'ns-1' AND name = 'name-1' ",
				"",
			},
		},
		{
			"with namespace and names",
			&internal.ListOptions{
				Namespaces: []string{"ns-1"},
				Names:      []string{"name-1", "name-2"},
			},
			expected{
				`SELECT * FROM "resources" WHERE namespace = 'ns-1' AND name IN ('name-1','name-2') `,
				"SELECT * FROM `resources` WHERE namespace = 'ns-1' AND name IN ('name-1','name-2') ",
				"",
			},
		},
		{
			name: "with since and before",
			listOptions: &internal.ListOptions{
				Since:  &metav1.Time{Time: since},
				Before: &metav1.Time{Time: before},
			},
			expected: expected{
				`SELECT * FROM "resources" WHERE created_at >= '2022-03-04 00:00:00' AND created_at < '2022-03-15 00:00:00' `,
				"SELECT * FROM `resources` WHERE created_at >= '2022-03-04 00:00:00' AND created_at < '2022-03-15 00:00:00' ",
				"",
			},
		},
	}

	for _, test := range tests {
		testApplyListOptionsToQuery(t, test.name, test.listOptions, test.expected)
	}
}

func TestApplyListOptionsToQuery_LabelSelector(t *testing.T) {
	tests := []struct {
		name          string
		labelSelector string
		expected      expected
	}{
		{
			"equal",
			"key1=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1' = 'value1' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) = 'value1' ",
				"",
			},
		},
		{
			"equal with complex key",
			"key1.io=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' = 'value1' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) = 'value1' ",
				"",
			},
		},
		{
			"equal with empty value",
			"key1.io=",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' = '' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) = '' ",
				"",
			},
		},
		{
			"not equal",
			"key1!=value1",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'metadata' -> 'labels' ->> 'key1' IS NULL OR "object" -> 'metadata' -> 'labels' ->> 'key1' != 'value1') `,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) != 'value1') ",
				"",
			},
		},
		{
			"in",
			"key1 in (value1, value2),key2=value2",

			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1' IN ('value1','value2') AND "object" -> 'metadata' -> 'labels' ->> 'key2' = 'value2' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) IN ('value1','value2') AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key2\"')) = 'value2' ",
				"",
			},
		},
		{
			"notin",
			"key1 notin (value1, value2),key2=value2",

			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'metadata' -> 'labels' ->> 'key1' IS NULL OR "object" -> 'metadata' -> 'labels' ->> 'key1' NOT IN ('value1','value2')) AND "object" -> 'metadata' -> 'labels' ->> 'key2' = 'value2' `,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) NOT IN ('value1','value2')) AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key2\"')) = 'value2' ",
				"",
			},
		},
		{
			"exist",
			"key1.io",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' IS NOT NULL `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) IS NOT NULL ",
				"",
			},
		},
		{
			"not exist",
			"!key1.io",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' IS NULL `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) IS NULL ",
				"",
			},
		},
	}

	for _, test := range tests {
		var listOptions = &internal.ListOptions{}
		selector, err := labels.Parse(test.labelSelector)
		if err != nil {
			t.Fatalf("labels.Parse() failed: %v", err)
		}
		listOptions.LabelSelector = selector

		testApplyListOptionsToQuery(t, test.name, listOptions, test.expected)
	}
}

func TestApplyListOptionsToQuery_EnhancedFieldSelector(t *testing.T) {
	tests := []struct {
		name          string
		fieldSelector string

		expected expected
	}{
		{
			"equal",
			"field1.field11=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' = 'value1' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) = 'value1' ",
				"",
			},
		},
		{
			"equal with complex key",
			"field1.field11['field12.io']=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' -> 'field11' ->> 'field12.io' = 'value1' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\".\"field12.io\"')) = 'value1' ",
				"",
			},
		},
		{
			"equal with empty value",
			"field1.field11=",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' = '' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) = '' ",
				"",
			},
		},
		{
			"not equal",
			"field1.field11!=value1",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'field1' ->> 'field11' IS NULL OR "object" -> 'field1' ->> 'field11' != 'value1') `,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) != 'value1') ",
				"",
			},
		},
		{
			"in",
			"field1.field11 in (value1, value2),field2=value2",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IN ('value1','value2') AND "object" ->> 'field2' = 'value2' `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IN ('value1','value2') AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field2\"')) = 'value2' ",
				"",
			},
		},
		{
			"notin",
			"field1.field11 notin (value1, value2),field2=value2",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'field1' ->> 'field11' IS NULL OR "object" -> 'field1' ->> 'field11' NOT IN ('value1','value2')) AND "object" ->> 'field2' = 'value2' `,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) NOT IN ('value1','value2')) AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field2\"')) = 'value2' ",
				"",
			},
		},
		{
			"exist",
			"field1.field11",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IS NOT NULL `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IS NOT NULL ",
				"",
			},
		},
		{
			"not exist",
			"!field1.field11",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IS NULL `,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IS NULL ",
				"",
			},
		},
	}

	for _, test := range tests {
		var listOptions = &internal.ListOptions{}
		selector, err := fields.Parse(test.fieldSelector)
		if err != nil {
			t.Fatalf("fields.Parse() failed: %v", err)
		}
		listOptions.EnhancedFieldSelector = selector

		testApplyListOptionsToQuery(t, test.name, listOptions, test.expected)
	}
}

func TestApplyListOptionsToQuery_FieldSelector(t *testing.T) {
	tests := []struct {
		name          string
		fieldSelector string

		expected expected
	}{
		{
			"equal",
			"field1.field11=value1",
			expected{
				`SELECT * FROM "resources" `,
				"SELECT * FROM `resources` ",
				"",
			},
		},
	}

	for _, test := range tests {
		var listOptions = &internal.ListOptions{}
		selector, err := kubefields.ParseSelector(test.fieldSelector)
		if err != nil {
			t.Fatalf("fields.Parse() failed: %v", err)
		}
		listOptions.FieldSelector = selector

		testApplyListOptionsToQuery(t, test.name, listOptions, test.expected)
	}
}

func TestApplyListOptionsToQuery_OrderBy(t *testing.T) {
	tests := []struct {
		name     string
		orderby  []internal.OrderBy
		expected expected
	}{
		{
			"asec",
			[]internal.OrderBy{
				{Field: "namespace"},
				{Field: "name"},
				{Field: "cluster"},
				{Field: "resource_version"},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY namespace,name,cluster,CAST(resource_version as decimal) `,
				"SELECT * FROM `resources` ORDER BY namespace,name,cluster,CAST(resource_version as decimal) ",
				"",
			},
		},
		{
			"dsec",
			[]internal.OrderBy{
				{Field: "namespace"},
				{Field: "name", Desc: true},
				{Field: "cluster", Desc: true},
				{Field: "resource_version", Desc: true},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY namespace,name DESC,cluster DESC,CAST(resource_version as decimal) DESC `,
				"SELECT * FROM `resources` ORDER BY namespace,name DESC,cluster DESC,CAST(resource_version as decimal) DESC ",
				"",
			},
		},
		{
			"with not support fields",
			[]internal.OrderBy{
				{Field: "name"},
				{Field: "group"},
				{Field: "cluster"},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY name,cluster `,
				"SELECT * FROM `resources` ORDER BY name,cluster ",
				"",
			},
		},
	}

	for _, test := range tests {
		listOptions := &internal.ListOptions{OrderBy: test.orderby}
		testApplyListOptionsToQuery(t, test.name, listOptions, test.expected)
	}
}

func TestApplyListOptionsToQuery_Page(t *testing.T) {
	tests := []struct {
		name     string
		limit    int64
		offset   string
		expected expected
	}{
		{
			"limit",
			10, "",
			expected{
				`SELECT * FROM "resources" LIMIT 10`,
				"SELECT * FROM `resources` LIMIT 10",
				"",
			},
		},
		{
			"limit 0",
			0, "",
			expected{
				`SELECT * FROM "resources" `,
				"SELECT * FROM `resources` ",
				"",
			},
		},
		{
			"limit -1",
			-1, "",
			expected{
				`SELECT * FROM "resources"`,
				"SELECT * FROM `resources`",
				"",
			},
		},
		{
			"limit -2",
			-2, "",
			expected{
				`SELECT * FROM "resources" `,
				"SELECT * FROM `resources` ",
				"",
			},
		},
		{
			"continue",
			0, "1",
			expected{
				`SELECT * FROM "resources" OFFSET 1`,
				"SELECT * FROM `resources` OFFSET 1",
				"",
			},
		},
		{
			"continue -1",
			0, "-1",
			expected{
				`SELECT * FROM "resources" `,
				"SELECT * FROM `resources` ",
				"",
			},
		},
		{
			"bad continue",
			0, "abdfdf",
			expected{
				`SELECT * FROM "resources" `,
				"SELECT * FROM `resources` ",
				"",
			},
		},
		{
			"limit an continue",
			10, "1",
			expected{
				`SELECT * FROM "resources" LIMIT 10 OFFSET 1`,
				"SELECT * FROM `resources` LIMIT 10 OFFSET 1",
				"",
			},
		},
	}

	for _, test := range tests {
		listOptions := &internal.ListOptions{}
		listOptions.Limit, listOptions.Continue = test.limit, test.offset
		testApplyListOptionsToQuery(t, test.name, listOptions, test.expected)
	}
}

// replace db.ToSQL
func toSQL(db *gorm.DB, options *internal.ListOptions, applyFn func(*gorm.DB, *internal.ListOptions) (*gorm.DB, error)) (string, error) {
	query := db.Session(&gorm.Session{DryRun: true}).Model(&Resource{})
	query, err := applyFn(query, options)
	if err != nil {
		return "", err
	}

	stmt := query.Find(interface{}(nil)).Statement
	return db.Dialector.Explain(stmt.SQL.String(), stmt.Vars...), nil
}

func assertError(t *testing.T, expectedErr string, err error) {
	if expectedErr != "" {
		if err == nil || err.Error() != expectedErr {
			t.Errorf("expected error: %q, but got: %#v", expectedErr, err)
		}
	} else if err != nil {
		t.Errorf("expected nil error, but got: %#v", err)
	}
}
