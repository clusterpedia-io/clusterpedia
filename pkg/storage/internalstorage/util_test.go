package internalstorage

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefields "k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	internal "github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/api/clusterpedia/fields"
)

func toFindQuery[P any](db *gorm.DB, options P, applyFn func(*gorm.DB, P) (*gorm.DB, error), dryRun bool) (*gorm.DB, error) {
	query := db.Session(&gorm.Session{DryRun: dryRun}).Model(&Resource{})
	query, err := applyFn(query, options)
	if err != nil {
		return nil, err
	}

	return query.Find(&Resource{}), nil
}

// replace db.ToSQL
func toSQL[P any](db *gorm.DB, options P, applyFn func(*gorm.DB, P) (*gorm.DB, error)) (string, error) {
	query, err := toFindQuery(db, options, applyFn, true)
	if err != nil {
		return "", err
	}

	return db.Dialector.Explain(query.Statement.SQL.String(), query.Statement.Vars...), nil
}

func assertSQL[P any](t *testing.T, db *gorm.DB, options P, applyFn func(*gorm.DB, P) (*gorm.DB, error), expectedQuery string, expectedError error) {
	sql, err := toSQL(db, options, applyFn)
	if expectedError == nil {
		require.NoError(t, err)
		assert.Equal(t, expectedQuery, sql)
	} else {
		require.Error(t, err)
		assert.Equal(t, expectedError, err)
	}
}

func toUnexplainedSQL[P any](db *gorm.DB, options P, applyFn func(*gorm.DB, P) (*gorm.DB, error)) (string, error) {
	query, err := toFindQuery(db, options, applyFn, true)
	if err != nil {
		return "", err
	}

	return query.Statement.SQL.String(), nil
}

func assertUnexplainedSQL[P any](t *testing.T, db *gorm.DB, options P, applyFn func(*gorm.DB, P) (*gorm.DB, error), expectedQuery string, expectedError error) {
	sql, err := toUnexplainedSQL(db, options, applyFn)
	if expectedError == nil {
		require.NoError(t, err)
		assert.Equal(t, expectedQuery, sql)
	} else {
		require.Error(t, err)
		assert.Equal(t, expectedError, err)
	}
}

func assertDatabaseExecutedSQL[P any](
	t *testing.T,
	db *gorm.DB,
	mock sqlmock.Sqlmock,
	options P,
	applyFn func(*gorm.DB, P) (*gorm.DB, error),
	expectedQuery string,
	args []driver.Value,
	expectedError error,
) {
	if expectedError == nil {
		mock.
			ExpectQuery(expectedQuery).
			WithArgs(args...).
			WillReturnRows(sqlmock.NewRows([]string{"id", "test_column", "test_column2", "test_column3"}))
	}

	query, err := toFindQuery(
		db,
		options,
		applyFn,
		false,
	)
	if expectedError == nil {
		require.NoError(t, err)
		require.NotNil(t, query)
	} else {
		require.Error(t, err)
		assert.Equal(t, expectedError, err)
	}

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func assertPostgresDatabaseExecutedSQL[P any](
	t *testing.T,
	options P,
	applyFn func(*gorm.DB, P) (*gorm.DB, error),
	expectedQuery string,
	args []driver.Value,
	expectedError error,
) {
	db, mock, err := newMockedPostgresDB()
	require.NoError(t, err)
	require.NotNil(t, db)
	require.NotNil(t, mock)

	assertDatabaseExecutedSQL(t, db, mock, options, applyFn, expectedQuery, args, expectedError)
}

func assertMySQLDatabaseExecutedSQL[P any](
	t *testing.T,
	version string,
	options P,
	applyFn func(*gorm.DB, P) (*gorm.DB, error),
	expectedQuery string,
	args []driver.Value,
	expectedError error,
) {
	db, mock, err := newMockedMySQLDB(version)
	require.NoError(t, err)
	require.NotNil(t, db)
	require.NotNil(t, mock)

	assertDatabaseExecutedSQL(t, db, mock, options, applyFn, expectedQuery, args, expectedError)
}

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
				`SELECT * FROM "resources" WHERE cluster = 'cluster-1'`,
				"SELECT * FROM `resources` WHERE cluster = 'cluster-1'",
				"",
			},
		},
		{
			"with clusters",
			&internal.ListOptions{
				ClusterNames: []string{"cluster-1", "cluster-2"},
			},
			expected{
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2')`,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2')",
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
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2') AND namespace = 'ns-1'`,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2') AND namespace = 'ns-1'",
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
				`SELECT * FROM "resources" WHERE cluster IN ('cluster-1','cluster-2') AND namespace IN ('ns-1','ns-2')`,
				"SELECT * FROM `resources` WHERE cluster IN ('cluster-1','cluster-2') AND namespace IN ('ns-1','ns-2')",
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
				`SELECT * FROM "resources" WHERE namespace = 'ns-1' AND name = 'name-1'`,
				"SELECT * FROM `resources` WHERE namespace = 'ns-1' AND name = 'name-1'",
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
				`SELECT * FROM "resources" WHERE namespace = 'ns-1' AND name IN ('name-1','name-2')`,
				"SELECT * FROM `resources` WHERE namespace = 'ns-1' AND name IN ('name-1','name-2')",
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
				`SELECT * FROM "resources" WHERE created_at >= '2022-03-04 00:00:00' AND created_at < '2022-03-15 00:00:00'`,
				"SELECT * FROM `resources` WHERE created_at >= '2022-03-04 00:00:00' AND created_at < '2022-03-15 00:00:00'",
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
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1' = 'value1'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) = 'value1'",
				"",
			},
		},
		{
			"equal with complex key",
			"key1.io=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' = 'value1'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) = 'value1'",
				"",
			},
		},
		{
			"equal with empty value",
			"key1.io=",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' = ''`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) = ''",
				"",
			},
		},
		{
			"not equal",
			"key1!=value1",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'metadata' -> 'labels' ->> 'key1' IS NULL OR "object" -> 'metadata' -> 'labels' ->> 'key1' != 'value1')`,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) != 'value1')",
				"",
			},
		},
		{
			"in",
			"key1 in (value1, value2),key2=value2",

			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1' IN ('value1','value2') AND "object" -> 'metadata' -> 'labels' ->> 'key2' = 'value2'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) IN ('value1','value2') AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key2\"')) = 'value2'",
				"",
			},
		},
		{
			"notin",
			"key1 notin (value1, value2),key2=value2",

			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'metadata' -> 'labels' ->> 'key1' IS NULL OR "object" -> 'metadata' -> 'labels' ->> 'key1' NOT IN ('value1','value2')) AND "object" -> 'metadata' -> 'labels' ->> 'key2' = 'value2'`,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1\"')) NOT IN ('value1','value2')) AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key2\"')) = 'value2'",
				"",
			},
		},
		{
			"exist",
			"key1.io",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' IS NOT NULL`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) IS NOT NULL",
				"",
			},
		},
		{
			"not exist",
			"!key1.io",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'metadata' -> 'labels' ->> 'key1.io' IS NULL`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"metadata\".\"labels\".\"key1.io\"')) IS NULL",
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
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' = 'value1'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) = 'value1'",
				"",
			},
		},
		{
			"equal with complex key",
			"field1.field11['field12.io']=value1",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' -> 'field11' ->> 'field12.io' = 'value1'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\".\"field12.io\"')) = 'value1'",
				"",
			},
		},
		{
			"equal with empty value",
			"field1.field11=",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' = ''`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) = ''",
				"",
			},
		},
		{
			"not equal",
			"field1.field11!=value1",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'field1' ->> 'field11' IS NULL OR "object" -> 'field1' ->> 'field11' != 'value1')`,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) != 'value1')",
				"",
			},
		},
		{
			"in",
			"field1.field11 in (value1, value2),field2=value2",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IN ('value1','value2') AND "object" ->> 'field2' = 'value2'`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IN ('value1','value2') AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field2\"')) = 'value2'",
				"",
			},
		},
		{
			"notin",
			"field1.field11 notin (value1, value2),field2=value2",
			expected{
				`SELECT * FROM "resources" WHERE ("object" -> 'field1' ->> 'field11' IS NULL OR "object" -> 'field1' ->> 'field11' NOT IN ('value1','value2')) AND "object" ->> 'field2' = 'value2'`,
				"SELECT * FROM `resources` WHERE (JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"') IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) NOT IN ('value1','value2')) AND JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field2\"')) = 'value2'",
				"",
			},
		},
		{
			"exist",
			"field1.field11",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IS NOT NULL`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IS NOT NULL",
				"",
			},
		},
		{
			"not exist",
			"!field1.field11",
			expected{
				`SELECT * FROM "resources" WHERE "object" -> 'field1' ->> 'field11' IS NULL`,
				"SELECT * FROM `resources` WHERE JSON_UNQUOTE(JSON_EXTRACT(`object`,'$.\"field1\".\"field11\"')) IS NULL",
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
				`SELECT * FROM "resources"`,
				"SELECT * FROM `resources`",
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
			"asc",
			[]internal.OrderBy{
				{Field: "namespace"},
				{Field: "name"},
				{Field: "cluster"},
				{Field: "resource_version"},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY namespace,name,cluster,CAST(resource_version as decimal)`,
				"SELECT * FROM `resources` ORDER BY namespace,name,cluster,CAST(resource_version as decimal)",
				"",
			},
		},
		{
			"desc",
			[]internal.OrderBy{
				{Field: "namespace"},
				{Field: "name", Desc: true},
				{Field: "cluster", Desc: true},
				{Field: "resource_version", Desc: true},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY namespace,name DESC,cluster DESC,CAST(resource_version as decimal) DESC`,
				"SELECT * FROM `resources` ORDER BY namespace,name DESC,cluster DESC,CAST(resource_version as decimal) DESC",
				"",
			},
		},
		{
			"order by custom fields asc",
			[]internal.OrderBy{
				{Field: "JSON_EXTRACT(object,'$.status.podIP')"},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY JSON_EXTRACT(object,'$.status.podIP')`,
				"SELECT * FROM `resources` ORDER BY JSON_EXTRACT(object,'$.status.podIP')",
				"",
			},
		},
		{
			"order by custom fields desc",
			[]internal.OrderBy{
				{Field: "JSON_EXTRACT(object,'$.status.podIP')", Desc: true},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY JSON_EXTRACT(object,'$.status.podIP') DESC`,
				"SELECT * FROM `resources` ORDER BY JSON_EXTRACT(object,'$.status.podIP') DESC",
				"",
			},
		},
		{
			"order by multiple fields",
			[]internal.OrderBy{
				{Field: "JSON_EXTRACT(object,'$.status.podIP')", Desc: true},
				{Field: "name"},
			},
			expected{
				`SELECT * FROM "resources" ORDER BY JSON_EXTRACT(object,'$.status.podIP') DESC,name`,
				"SELECT * FROM `resources` ORDER BY JSON_EXTRACT(object,'$.status.podIP') DESC,name",
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
				`SELECT * FROM "resources"`,
				"SELECT * FROM `resources`",
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
				`SELECT * FROM "resources"`,
				"SELECT * FROM `resources`",
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
				`SELECT * FROM "resources"`,
				"SELECT * FROM `resources`",
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

func newURLQueryFieldWhereSQLJSONParamsBase64DecodingError() (string, *apierrors.StatusError) {
	corruptedBase64Payload := "A==="
	_, corruptedBase64PayloadError := base64.StdEncoding.DecodeString(corruptedBase64Payload)

	return corruptedBase64Payload, apierrors.NewInvalid(
		schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
		"urlQuery",
		field.ErrorList{
			field.Invalid(
				field.NewPath(URLQueryFieldWhereSQLJSONParams),
				corruptedBase64Payload,
				fmt.Sprintf("failed to decode base64 string: %v", corruptedBase64PayloadError),
			),
		},
	)
}

func newURLQueryFieldWhereSQLJSONParamsUnmarshalError(originalJSONPayloadContent string) (string, *apierrors.StatusError) {
	base64EncodedContent := base64.StdEncoding.EncodeToString([]byte(originalJSONPayloadContent))
	var unmarshalDest []any
	unmarshalError := json.Unmarshal([]byte(originalJSONPayloadContent), &unmarshalDest)

	return base64EncodedContent, apierrors.NewInvalid(
		schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
		"urlQuery",
		field.ErrorList{
			field.Invalid(
				field.NewPath(URLQueryFieldWhereSQLJSONParams),
				base64EncodedContent,
				fmt.Sprintf("failed to unmarshal decoded base64 string to JSON array: %v", unmarshalError),
			),
		},
	)
}

func newURLQueryFieldWhereSQLStatementRequiredError() *apierrors.StatusError {
	var emptyWhereSQLStmt []string

	return apierrors.NewInvalid(
		schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
		"urlQuery",
		field.ErrorList{
			field.Invalid(
				field.NewPath(URLQueryFieldWhereSQLStatement),
				emptyWhereSQLStmt,
				fmt.Sprintf("required when either %s or %s was provided", URLQueryFieldWhereSQLParam, URLQueryFieldWhereSQLJSONParams),
			),
		},
	)
}

func TestNewURLQueryWhereSQLParamsFromURLValues(t *testing.T) {
	type testCase struct {
		name      string
		urlValues url.Values

		expectedAPIError       *apierrors.StatusError
		expectedAPIErrorString string

		expectedWhereSQL           string
		expectedWhereSQLStatement  string
		expectedWhereSQLParams     []string
		expectedWhereSQLJSONParams []any
	}

	base64DecodingErrorPayload, base64DecodingError := newURLQueryFieldWhereSQLJSONParamsBase64DecodingError()
	jsonUnmarshalErrorInvalidJSONErrorPayload, jsonUnmarshalErrorInvalidJSONError := newURLQueryFieldWhereSQLJSONParamsUnmarshalError("invalid-json")
	jsonUnmarshalErrorNonJSONArrayErrorPayload, jsonUnmarshalErrorNonJSONArrayError := newURLQueryFieldWhereSQLJSONParamsUnmarshalError(`{"key1": "value1", "key2": "value2"}`)

	validJSONPayload := `["value1", "value2"]`
	encodedBase64ValidJSONPayload := base64.StdEncoding.EncodeToString([]byte(validJSONPayload))
	validJSONPayloadUnmarshal := []any{"value1", "value2"}

	urlQueryFieldWhereSQLStatementRequiredError := newURLQueryFieldWhereSQLStatementRequiredError()

	testCases := []testCase{
		{
			name: "WhereSQLWithoutAnyParams",
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = ?"},
			},
			expectedWhereSQL: "test_column = ?",
		},
		{
			name: "WhereSQLStatementWithoutAnyParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
			},
			expectedWhereSQLStatement: "test_column = ?",
		},
		{
			name: "WhereSQLStatementWithWhereSQLParam",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1"},
			},
			expectedWhereSQLStatement: "test_column = ?",
			expectedWhereSQLParams:    []string{"value1"},
		},
		{
			name: "WhereSQLStatementWithMultipleWhereSQLParam",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ? AND test_column2 = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1", "value2"},
			},
			expectedWhereSQLStatement: "test_column = ? AND test_column2 = ?",
			expectedWhereSQLParams:    []string{"value1", "value2"},
		},
		{
			name: "WhereSQLStatementWithWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{encodedBase64ValidJSONPayload},
			},
			expectedWhereSQLStatement:  "test_column = ?",
			expectedWhereSQLJSONParams: validJSONPayloadUnmarshal,
		},
		{
			name: "WhereSQLStatementWithMultipleWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{encodedBase64ValidJSONPayload, encodedBase64ValidJSONPayload},
			},
			expectedWhereSQLStatement:  "test_column = ?",
			expectedWhereSQLJSONParams: validJSONPayloadUnmarshal,
		},
		{
			name: "WhereSQLStatementWithBothWhereSQLParamAndWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:      []string{"value1"},
				URLQueryFieldWhereSQLJSONParams: []string{encodedBase64ValidJSONPayload},
			},
			expectedWhereSQLStatement:  "test_column = ?",
			expectedWhereSQLParams:     []string{"value1"},
			expectedWhereSQLJSONParams: validJSONPayloadUnmarshal,
		},
		{
			name: "WhereSQLStatementWithBothMultipleWhereSQLParamAndWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ? AND test_column2 = ?"},
				URLQueryFieldWhereSQLParam:      []string{"value1", "value2"},
				URLQueryFieldWhereSQLJSONParams: []string{encodedBase64ValidJSONPayload},
			},
			expectedWhereSQLStatement:  "test_column = ? AND test_column2 = ?",
			expectedWhereSQLParams:     []string{"value1", "value2"},
			expectedWhereSQLJSONParams: validJSONPayloadUnmarshal,
		},
		{
			name: "CorruptedBase64PayloadValueOfWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{base64DecodingErrorPayload},
			},
			expectedAPIError:       base64DecodingError,
			expectedAPIErrorString: base64DecodingError.Error(),
		},
		{
			name: "InvalidBase64EncodedJSONPayloadValueOfWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{jsonUnmarshalErrorInvalidJSONErrorPayload},
			},
			expectedAPIError:       jsonUnmarshalErrorInvalidJSONError,
			expectedAPIErrorString: jsonUnmarshalErrorInvalidJSONError.Error(),
		},
		{
			name: "NonJSONArrayBase64EncodedJSONPayloadValueOfWhereSQLJSONParams",
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{jsonUnmarshalErrorNonJSONArrayErrorPayload},
			},
			expectedAPIError:       jsonUnmarshalErrorNonJSONArrayError,
			expectedAPIErrorString: jsonUnmarshalErrorNonJSONArrayError.Error(),
		},
		{
			name: "WhereSQLParamsWithoutWhereSQLStatement",
			urlValues: url.Values{
				URLQueryFieldWhereSQLParam: []string{"value1"},
			},
			expectedAPIError:       urlQueryFieldWhereSQLStatementRequiredError,
			expectedAPIErrorString: urlQueryFieldWhereSQLStatementRequiredError.Error(),
		},
		{
			name: "WhereSQLJSONParamsWithoutWhereSQLStatement",
			urlValues: url.Values{
				URLQueryFieldWhereSQLJSONParams: []string{encodedBase64ValidJSONPayload},
			},
			expectedAPIError:       urlQueryFieldWhereSQLStatementRequiredError,
			expectedAPIErrorString: urlQueryFieldWhereSQLStatementRequiredError.Error(),
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			params, err := NewURLQueryWhereSQLParamsFromURLValues(tc.urlValues)
			if tc.expectedAPIError == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)

				apiError, ok := err.(*apierrors.StatusError)
				require.True(t, ok)
				require.NotNil(t, apiError)

				assert.Equal(t, tc.expectedAPIError, apiError)
				assert.EqualError(t, err, tc.expectedAPIErrorString)
			}

			assert.Equal(t, tc.expectedWhereSQL, params.WhereSQL)
			assert.Equal(t, tc.expectedWhereSQLStatement, params.WhereSQLStatement)
			assert.Equal(t, tc.expectedWhereSQLParams, params.WhereSQLParams)
			assert.Equal(t, tc.expectedWhereSQLJSONParams, params.WhereSQLJSONParams)
		})
	}
}

type testSQLQueriesAssertionTestCase struct {
	ignorePostgres                 bool
	postgresRawQuery               string
	postgresUnexplainedRawQuery    string
	postgresSqlmockDBExecutedQuery string
	postgresSqlmockDBExecutedArgs  []driver.Value

	ignoreMySQL                 bool
	mysqlRawQuery               string
	mysqlUnexplainedRawQuery    string
	mysqlSqlmockDBExecutedQuery string
	mysqlSqlmockDBExecutedArgs  []driver.Value

	expectedError error
}

func testSQLQueriesAssertion[P any](t *testing.T, params P, testCase testSQLQueriesAssertionTestCase, applyFn func(*gorm.DB, P) (*gorm.DB, error)) {
	t.Run("Postgres", func(t *testing.T) {
		t.Run("RawQuery", func(t *testing.T) {
			if testCase.ignorePostgres {
				t.Skip("skipped due to explicit ignoring flag")
				return
			}

			assertSQL(t, postgresDB, params, applyFn, testCase.postgresRawQuery, testCase.expectedError)
		})

		t.Run("UnexplainedRawQuery", func(t *testing.T) {
			if testCase.ignorePostgres {
				t.Skip("skipped due to explicit ignoring flag")
				return
			}

			assertUnexplainedSQL(t, postgresDB, params, applyFn, testCase.postgresUnexplainedRawQuery, testCase.expectedError)
		})

		t.Run("DBExecutedQuery", func(t *testing.T) {
			if testCase.ignorePostgres {
				t.Skip("skipped due to explicit ignoring flag")
				return
			}

			assertPostgresDatabaseExecutedSQL(t, params, applyFn, testCase.postgresSqlmockDBExecutedQuery, testCase.postgresSqlmockDBExecutedArgs, testCase.expectedError)
		})
	})

	for version, mysqlDB := range mysqlDBs {
		version := version
		mysqlDB := mysqlDB

		t.Run(fmt.Sprintf("MySQL%s", version), func(t *testing.T) {
			t.Run("RawQuery", func(t *testing.T) {
				if testCase.ignoreMySQL {
					t.Skip("skipped due to explicit ignoring flag")
					return
				}

				assertSQL(t, mysqlDB, params, applyFn, testCase.mysqlRawQuery, testCase.expectedError)
			})

			t.Run("UnexplainedRawQuery", func(t *testing.T) {
				if testCase.ignoreMySQL {
					t.Skip("skipped due to explicit ignoring flag")
					return
				}

				assertUnexplainedSQL(t, mysqlDB, params, applyFn, testCase.mysqlUnexplainedRawQuery, testCase.expectedError)
			})

			t.Run("DBExecutedQuery", func(t *testing.T) {
				if testCase.ignoreMySQL {
					t.Skip("skipped due to explicit ignoring flag")
					return
				}

				assertMySQLDatabaseExecutedSQL(t, version, params, applyFn, testCase.mysqlSqlmockDBExecutedQuery, testCase.mysqlSqlmockDBExecutedArgs, testCase.expectedError)
			})
		})
	}
}

type applyListOptionsURLQueryParameterizedQueryToWhereClauseTestCase struct {
	testSQLQueriesAssertionTestCase

	name string

	whereSQLParams     URLQueryWhereSQLParams
	whereSQLJSONParams URLQueryWhereSQLParams
}

func TestApplyListOptionsURLQueryParameterizedQueryToWhereClause(t *testing.T) {
	commonTestCases := []applyListOptionsURLQueryParameterizedQueryToWhereClauseTestCase{
		{
			name: "WithoutWhereSQLStatement",

			whereSQLParams:     URLQueryWhereSQLParams{},
			whereSQLJSONParams: URLQueryWhereSQLParams{},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\"",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\"",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\"",

				mysqlRawQuery:               "SELECT * FROM `resources`",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources`",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources`",
			},
		},
		{
			name: "WithoutAnyParameters/SingleWhereClause",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1",
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1",
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1",
			},
		},
		{
			name: "WithoutAnyParameters/MultipleWhereClauses",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1 AND created_at > value2",
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1 AND created_at > value2",
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1 AND created_at > value2",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1 AND created_at > value2",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1 AND created_at > value2",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1 AND created_at > value2",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1 AND created_at > value2",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1 AND created_at > value2",
			},
		},
		{
			name: "WithoutAnyParameters/ExplicitPotentialSQLInjectionPayload",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1 OR 1=1",
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = value1 OR 1=1",
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1 OR 1=1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1 OR 1=1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1 OR 1=1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1 OR 1=1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1 OR 1=1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1 OR 1=1",
			},
		},
		{
			name: "WithParameters/SingleParameter",
			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ?",
				WhereSQLParams:    []string{"value1"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ?",
				WhereSQLJSONParams: []any{"value1"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1"},
			},
		},
		{
			name: "WithParameters/MultipleParameters",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ? AND test_column2 = ?",
				WhereSQLParams:    []string{"value1", "value2"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ? AND test_column2 = ?",
				WhereSQLJSONParams: []any{"value1", "value2"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1' AND test_column2 = 'value2'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1 AND test_column2 = $2",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1 AND test_column2 = \\$2",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1' AND test_column2 = 'value2'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ? AND test_column2 = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\? AND test_column2 = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},
			},
		},
		{
			name: "WithParameters/Mismatched",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ? AND test_column2 = ?",
				WhereSQLParams:    []string{"value1"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ? AND test_column2 = ?",
				WhereSQLJSONParams: []any{"value1"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1' AND test_column2 = ?",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1 AND test_column2 = ?",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1 AND test_column2 = \\?",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1' AND test_column2 = ?",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ? AND test_column2 = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\? AND test_column2 = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1"},
			},
		},

		// Injection test cases
		// This is the commonly used case where OR 1=1 will make the where clause always true if not well escaped.
		{
			name: "DefendSQLInjectionPayload/AlwaysTrue",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ?",
				WhereSQLParams:    []string{" OR ('1' = '1"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ?",
				WhereSQLJSONParams: []any{" OR ('1' = '1"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = ' OR (\\'1\\' = \\'1'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{" OR ('1' = '1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = ' OR (\\'1\\' = \\'1'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{" OR ('1' = '1"},
			},
		},
		// Assume that there is a 'test_column2 = true' where clause after the possible injected where clause
		// appending -- to the end of the where clause will comment out the rest of the query if not well escaped.
		{
			name: "DefendSQLInjectionPayload/Comment",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ? AND test_column2 = true",
				WhereSQLParams:    []string{" OR ('1' = '1'); --"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ? AND test_column2 = true",
				WhereSQLJSONParams: []any{" OR ('1' = '1'); --"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = ' OR (\\'1\\' = \\'1\\'); --' AND test_column2 = true",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1 AND test_column2 = true",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{" OR ('1' = '1'); --"},

				ignoreMySQL: true,
			},
		},
		// Same as above but for MySQL
		{
			name: "DefendSQLInjectionPayload/Comment",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ? AND test_column2 = true",
				WhereSQLParams:    []string{" OR ('1' = '1'); #"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ? AND test_column2 = true",
				WhereSQLJSONParams: []any{" OR ('1' = '1'); #"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				ignorePostgres: true,

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = ' OR (\\'1\\' = \\'1\\'); #' AND test_column2 = true",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ? AND test_column2 = true",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\? AND test_column2 = true",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{" OR ('1' = '1'); #"},
			},
		},
		// This is a bit complicated. Assume that there are three columns in the table, test_column, test_column2 and test_column3.
		// Attack could use
		//  UNION SELECT NULL--
		//  UNION SELECT NULL,NULL–-
		//  UNION SELECT NULL,NULL--
		//  UNION SELECT NULL,NULL,NULL,NULL--
		// to find out the number of columns in the table. And then use
		//  UNION SELECT 'a' ,NULL,NULL,NULL--
		//  UNION SELECT NULL,'a',NULL,NULL--
		//  UNION SELECT NULL,NULL,'a',NULL--
		//  UNION SELECT NULL,NULL,NULL,'a'--
		// to find out the types of the columns in the table.
		// Finally, attacker could use
		//  UNION SELECT other_column1, other_column2, other_column3 FROM other_table
		// to extract data from other tables.
		{
			name: "DefendSQLInjectionPayload/UNIONBased/ColumnCountDetermination",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ?",
				WhereSQLParams:    []string{" UNION SELECT NULL,NULL,NULL;--"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ?",
				WhereSQLJSONParams: []any{" UNION SELECT NULL,NULL,NULL;--"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = ' UNION SELECT NULL,NULL,NULL;--'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT NULL,NULL,NULL;--"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = ' UNION SELECT NULL,NULL,NULL;--'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT NULL,NULL,NULL;--"},
			},
		},
		{
			name: "DefendSQLInjectionPayload/UNIONBased/ColumnTypeDetermination",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ?",
				WhereSQLParams:    []string{" UNION SELECT 'a',NULL,NULL;--"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ?",
				WhereSQLJSONParams: []any{" UNION SELECT 'a',NULL,NULL;--"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = ' UNION SELECT \\'a\\',NULL,NULL;--'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT 'a',NULL,NULL;--"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = ' UNION SELECT \\'a\\',NULL,NULL;--'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT 'a',NULL,NULL;--"},
			},
		},
		{
			name: "DefendSQLInjectionPayload/UNIONBased/UNIONAllOverride",

			whereSQLParams: URLQueryWhereSQLParams{
				WhereSQLStatement: "test_column = ?",
				WhereSQLParams:    []string{" UNION SELECT other_column1, other_column2, other_column3 FROM other_table"},
			},
			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column = ?",
				WhereSQLJSONParams: []any{" UNION SELECT other_column1, other_column2, other_column3 FROM other_table"},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = ' UNION SELECT other_column1, other_column2, other_column3 FROM other_table'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT other_column1, other_column2, other_column3 FROM other_table"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = ' UNION SELECT other_column1, other_column2, other_column3 FROM other_table'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{" UNION SELECT other_column1, other_column2, other_column3 FROM other_table"},
			},
		},
	}

	for _, tc := range commonTestCases {
		tc := tc

		t.Run(fmt.Sprintf("%s/%s", "WhereSQLParams", tc.name), func(t *testing.T) {
			testSQLQueriesAssertion(
				t,
				tc.whereSQLParams,
				tc.testSQLQueriesAssertionTestCase,
				func(query *gorm.DB, params URLQueryWhereSQLParams) (*gorm.DB, error) {
					return applyListOptionsURLQueryParameterizedQueryToWhereClause(query, params), nil
				},
			)
		})
	}
	for _, tc := range commonTestCases {
		tc := tc

		t.Run(fmt.Sprintf("%s/%s", "WhereSQLJSONParams", tc.name), func(t *testing.T) {
			testSQLQueriesAssertion(
				t,
				tc.whereSQLJSONParams,
				tc.testSQLQueriesAssertionTestCase,
				func(query *gorm.DB, params URLQueryWhereSQLParams) (*gorm.DB, error) {
					return applyListOptionsURLQueryParameterizedQueryToWhereClause(query, params), nil
				},
			)
		})
	}

	now := time.Now()

	advancedWhereClauseWithWhereSQLJSONParamsTestCases := []applyListOptionsURLQueryParameterizedQueryToWhereClauseTestCase{
		{
			name: "WithWhereSQLJSONParameters/IN",

			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column IN ?",
				WhereSQLJSONParams: []any{[]string{"value1", "value2"}},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column IN ('value1','value2')",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column IN ($1,$2)",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column IN \\(\\$1,\\$2\\)",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column IN ('value1','value2')",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column IN (?,?)",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column IN \\(\\?,\\?\\)",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},
			},
		},
		{
			name: "WithWhereSQLJSONParameters/NOT-IN",

			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column NOT IN ?",
				WhereSQLJSONParams: []any{[]string{"value1", "value2"}},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column NOT IN ('value1','value2')",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column NOT IN ($1,$2)",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column NOT IN \\(\\$1,\\$2\\)",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column NOT IN ('value1','value2')",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column NOT IN (?,?)",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column NOT IN \\(\\?,\\?\\)",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1", "value2"},
			},
		},
		{
			name: "WithWhereSQLJSONParameters/SpecialDataTypes/time.Time",

			whereSQLJSONParams: URLQueryWhereSQLParams{
				WhereSQLStatement:  "test_column <= ?",
				WhereSQLJSONParams: []any{now},
			},

			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               fmt.Sprintf("SELECT * FROM \"resources\" WHERE test_column <= '%s'", now.Format("2006-01-02 15:04:05.999")),
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column <= $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column <= \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{now},

				mysqlRawQuery:               fmt.Sprintf("SELECT * FROM `resources` WHERE test_column <= '%s'", now.Format("2006-01-02 15:04:05.999")),
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column <= ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column <= \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{now},
			},
		},
	}

	for _, tc := range advancedWhereClauseWithWhereSQLJSONParamsTestCases {
		tc := tc
		t.Run(fmt.Sprintf("%s/%s", "WhereSQLJSONParams", tc.name), func(t *testing.T) {
			testSQLQueriesAssertion(
				t,
				tc.whereSQLJSONParams,
				tc.testSQLQueriesAssertionTestCase,
				func(query *gorm.DB, params URLQueryWhereSQLParams) (*gorm.DB, error) {
					return applyListOptionsURLQueryParameterizedQueryToWhereClause(query, params), nil
				},
			)
		})
	}
}

func TestApplyListOptionsURLQueryToWhereClause(t *testing.T) {
	type testCase struct {
		testSQLQueriesAssertionTestCase

		name string

		allowRawSQLQueryEnabled           bool
		allowParameterizedSQLQueryEnabled bool
		urlValues                         url.Values
	}

	corruptedBase64Payload := "A==="
	_, corruptedBase64PayloadError := base64.StdEncoding.DecodeString(corruptedBase64Payload)
	require.Error(t, corruptedBase64PayloadError)

	featureGateRelatedTestCases := []testCase{
		{
			name:                              "WithURLQueryWhereSQL/NoFeatureGateEnabled",
			allowRawSQLQueryEnabled:           false,
			allowParameterizedSQLQueryEnabled: false,
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\"",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\"",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\"",

				mysqlRawQuery:               "SELECT * FROM `resources`",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources`",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources`",
			},
		},
		{
			name:                              "WithURLQueryFieldWhereSQLStatement/NoFeatureGateEnabled",
			allowRawSQLQueryEnabled:           false,
			allowParameterizedSQLQueryEnabled: false,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\"",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\"",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\"",

				mysqlRawQuery:               "SELECT * FROM `resources`",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources`",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources`",
			},
		},
		{
			name:                              "WithURLQueryWhereSQL/AllowRawSQLQueryEnabled",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: false,
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1",
			},
		},
		{
			name:                              "WithURLQueryFieldWhereSQLStatementWithParams/AllowRawSQLQueryEnabled",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: false,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1"},
			},
		},
		{
			name:                              "WithURLQueryWhereSQL/AllowParameterizedSQLQueryEnabled",
			allowRawSQLQueryEnabled:           false,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\"",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\"",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\"",

				mysqlRawQuery:               "SELECT * FROM `resources`",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources`",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources`",
			},
		},
		{
			name:                              "WithURLQueryFieldWhereSQLStatementWithParams/AllowParameterizedSQLQueryEnabled",
			allowRawSQLQueryEnabled:           false,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1"},
			},
		},
	}

	for _, tc := range featureGateRelatedTestCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			testSQLQueriesAssertion(
				t,
				tc.urlValues,
				tc.testSQLQueriesAssertionTestCase,
				func(query *gorm.DB, urlValues url.Values) (*gorm.DB, error) {
					return applyListOptionsURLQueryToWhereClause(query, urlValues, tc.allowRawSQLQueryEnabled, tc.allowParameterizedSQLQueryEnabled)
				},
			)
		})
	}

	urlQueryFieldWhereSQLJSONParamsBase64DecodingErrorPayload, urlQueryFieldWhereSQLJSONParamsBase64DecodingError := newURLQueryFieldWhereSQLJSONParamsBase64DecodingError()

	testCases := []testCase{
		{
			name:                              "Empty",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues:                         url.Values{},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\"",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\"",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\"",

				mysqlRawQuery:               "SELECT * FROM `resources`",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources`",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources`",
			},
		},
		{
			name:                              "WhereSQLStatementWithoutParams",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1",
			},
		},
		{
			name:                              "WhereSQLWithoutParams",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1",
			},
		},
		{
			name:                              "WhereSQLWithParams",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryWhereSQL: []string{"test_column = value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = value1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = value1",

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = value1",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = value1",
			},
		},
		{
			name:                              "WhereSQLStatementWithParams",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement: []string{"test_column = ?"},
				URLQueryFieldWhereSQLParam:     []string{"value1"},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				postgresRawQuery:               "SELECT * FROM \"resources\" WHERE test_column = 'value1'",
				postgresUnexplainedRawQuery:    "SELECT * FROM \"resources\" WHERE test_column = $1",
				postgresSqlmockDBExecutedQuery: "SELECT \\* FROM \"resources\" WHERE test_column = \\$1",
				postgresSqlmockDBExecutedArgs:  []driver.Value{"value1"},

				mysqlRawQuery:               "SELECT * FROM `resources` WHERE test_column = 'value1'",
				mysqlUnexplainedRawQuery:    "SELECT * FROM `resources` WHERE test_column = ?",
				mysqlSqlmockDBExecutedQuery: "SELECT \\* FROM `resources` WHERE test_column = \\?",
				mysqlSqlmockDBExecutedArgs:  []driver.Value{"value1"},
			},
		},
		{
			name:                              "ErrorHandling",
			allowRawSQLQueryEnabled:           true,
			allowParameterizedSQLQueryEnabled: true,
			urlValues: url.Values{
				URLQueryFieldWhereSQLStatement:  []string{"test_column = ?"},
				URLQueryFieldWhereSQLJSONParams: []string{urlQueryFieldWhereSQLJSONParamsBase64DecodingErrorPayload},
			},
			testSQLQueriesAssertionTestCase: testSQLQueriesAssertionTestCase{
				expectedError: urlQueryFieldWhereSQLJSONParamsBase64DecodingError,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			testSQLQueriesAssertion(
				t,
				tc.urlValues,
				tc.testSQLQueriesAssertionTestCase,
				func(query *gorm.DB, urlValues url.Values) (*gorm.DB, error) {
					return applyListOptionsURLQueryToWhereClause(query, urlValues, tc.allowRawSQLQueryEnabled, tc.allowParameterizedSQLQueryEnabled)
				},
			)
		})
	}
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
