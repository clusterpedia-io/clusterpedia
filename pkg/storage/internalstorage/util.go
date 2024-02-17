package internalstorage

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

const (
	SearchLabelFuzzyName = "internalstorage.clusterpedia.io/fuzzy-name"

	// Raw query
	URLQueryWhereSQL = "whereSQL"
	// Parameterized query
	URLQueryFieldWhereSQLStatement  = "whereSQLStatement"
	URLQueryFieldWhereSQLParam      = "whereSQLParam"
	URLQueryFieldWhereSQLJSONParams = "whereSQLJSONParams"
)

type URLQueryWhereSQLParams struct {
	// Raw query
	WhereSQL string
	// Parameterized query
	WhereSQLStatement  string
	WhereSQLParams     []string
	WhereSQLJSONParams []any
}

// NewURLQueryWhereSQLParamsFromURLValues resolves parameters from passed in url.Values.
// A k8s.io/apimachinery/pkg/api/errors.StatusError will be returned if decoding or unmarshalling failed
// only when the value of "whereSQLJSONParams" is present.
//
// It recognizes the following query fields for parameters:
//
//	"whereSQL"
//	"whereSQLStatement"
//	"whereSQLParam"
//	"whereSQLJSONParams"
func NewURLQueryWhereSQLParamsFromURLValues(urlQuery url.Values) (URLQueryWhereSQLParams, error) {
	var params URLQueryWhereSQLParams

	whereClause, ok := urlQuery[URLQueryWhereSQL]
	if ok && len(whereClause) > 0 {
		params.WhereSQL = whereClause[0]
	}

	whereClauseStatement, ok := urlQuery[URLQueryFieldWhereSQLStatement]
	if ok && len(whereClauseStatement) > 0 {
		params.WhereSQLStatement = whereClauseStatement[0]
	}

	whereClauseParams, ok := urlQuery[URLQueryFieldWhereSQLParam]
	if ok {
		params.WhereSQLParams = whereClauseParams
	}

	whereClauseJSONParams, ok := urlQuery[URLQueryFieldWhereSQLJSONParams]
	if ok && len(whereClauseJSONParams) > 0 {
		decodedBytesContent, err := base64.StdEncoding.DecodeString(whereClauseJSONParams[0])
		if err != nil {
			return URLQueryWhereSQLParams{}, apierrors.NewInvalid(
				schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
				"urlQuery",
				field.ErrorList{
					field.Invalid(
						field.NewPath(URLQueryFieldWhereSQLJSONParams),
						whereClauseJSONParams[0],
						fmt.Sprintf("failed to decode base64 string: %v", err),
					),
				},
			)
		}

		params.WhereSQLJSONParams = make([]any, 0)
		err = json.Unmarshal(decodedBytesContent, &params.WhereSQLJSONParams)
		if err != nil {
			return URLQueryWhereSQLParams{}, apierrors.NewInvalid(
				schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
				"urlQuery",
				field.ErrorList{
					field.Invalid(
						field.NewPath(URLQueryFieldWhereSQLJSONParams),
						whereClauseJSONParams[0],
						fmt.Sprintf("failed to unmarshal decoded base64 string to JSON array: %v", err),
					),
				},
			)
		}
	}

	if (len(params.WhereSQLParams) > 0 || len(params.WhereSQLJSONParams) > 0) && params.WhereSQLStatement == "" {
		return URLQueryWhereSQLParams{}, apierrors.NewInvalid(
			schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"},
			"urlQuery",
			field.ErrorList{
				field.Invalid(
					field.NewPath(URLQueryFieldWhereSQLStatement),
					whereClauseStatement,
					fmt.Sprintf("required when either %s or %s was provided", URLQueryFieldWhereSQLParam, URLQueryFieldWhereSQLJSONParams),
				),
			},
		)
	}

	return params, nil
}

func applyListOptionsURLQueryParameterizedQueryToWhereClause(query *gorm.DB, params URLQueryWhereSQLParams) *gorm.DB {
	if params.WhereSQLStatement == "" {
		return query
	}

	// If a string of numbers is passed in from SQL, the query will be taken as ID by default.
	// If the SQL contains English letter, it will be passed in as column.

	if len(params.WhereSQLJSONParams) > 0 {
		return query.Where(params.WhereSQLStatement, params.WhereSQLJSONParams...)
	}
	if len(params.WhereSQLParams) > 0 {
		anyParameters := make([]any, len(params.WhereSQLParams))

		for i := range params.WhereSQLParams {
			anyParameters[i] = params.WhereSQLParams[i]
		}

		return query.Where(params.WhereSQLStatement, anyParameters...)
	}

	return query.Where(params.WhereSQLStatement)
}

// applyListOptionsURLQueryToWhereClause applies the where sql related parameters from url query to the where clause of the query.
//
// By design, both the parameters of whereSQLStatement and whereSQL will be accepted and be part of the query in order when
// AllowRawSQLQuery feature gate is enabled, and only whereSQLStatement will be accepted and be part of the query when
// AllowParameterizedSQLQuery feature gate is enabled.
func applyListOptionsURLQueryToWhereClause(query *gorm.DB, urlValues url.Values, allowRawSQLQueryEnabled bool, allowParameterizedSQLQueryEnabled bool) (*gorm.DB, error) {
	if !allowRawSQLQueryEnabled && !allowParameterizedSQLQueryEnabled {
		return query, nil
	}

	urlQueryParams, err := NewURLQueryWhereSQLParamsFromURLValues(urlValues)
	if err != nil {
		return query, err
	}

	if allowRawSQLQueryEnabled {
		// use parameterized query first if statement was provided
		//
		// since users will need to migrate from caller (business) side first and make their transition
		// to the newly added feature gate AllowParameterizedSQLQuery step by step, therefore a compatible
		// implementation is required here to allow the migration and transition from caller (business) side
		// while the existing feature gates that enabled for Clusterpedia deployment can be left as untouched
		// and keep working as expected.
		if urlQueryParams.WhereSQLStatement != "" {
			return applyListOptionsURLQueryParameterizedQueryToWhereClause(query, urlQueryParams), nil
		}
		// otherwise, fallbacks to raw query
		if urlQueryParams.WhereSQL != "" {
			return query.Where(urlQueryParams.WhereSQL), nil
		}
	}

	if allowParameterizedSQLQueryEnabled && urlQueryParams.WhereSQLStatement != "" {
		return applyListOptionsURLQueryParameterizedQueryToWhereClause(query, urlQueryParams), nil
	}

	return query, nil
}

func applyListOptionsToQuery(query *gorm.DB, opts *internal.ListOptions, applyFn func(query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error)) (int64, *int64, *gorm.DB, error) {
	switch len(opts.ClusterNames) {
	case 0:
	case 1:
		query = query.Where("cluster = ?", opts.ClusterNames[0])
	default:
		query = query.Where("cluster IN ?", opts.ClusterNames)
	}

	switch len(opts.Namespaces) {
	case 0:
	case 1:
		query = query.Where("namespace = ?", opts.Namespaces[0])
	default:
		query = query.Where("namespace IN ?", opts.Namespaces)
	}

	switch len(opts.Names) {
	case 0:
	case 1:
		query = query.Where("name = ?", opts.Names[0])
	default:
		query = query.Where("name IN ?", opts.Names)
	}

	if opts.Since != nil {
		query = query.Where("created_at >= ?", opts.Since.Time.UTC())
	}

	if opts.Before != nil {
		query = query.Where("created_at < ?", opts.Before.Time.UTC())
	}

	query, err := applyListOptionsURLQueryToWhereClause(
		query,
		opts.URLQuery,
		utilfeature.DefaultMutableFeatureGate.Enabled(AllowRawSQLQuery),
		utilfeature.DefaultMutableFeatureGate.Enabled(AllowParameterizedSQLQuery),
	)
	if err != nil {
		return 0, nil, nil, err
	}

	if opts.LabelSelector != nil {
		if requirements, selectable := opts.LabelSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				values := requirement.Values().List()
				jsonQuery := JSONQuery("object", "metadata", "labels", requirement.Key())
				switch requirement.Operator() {
				case selection.Exists:
					jsonQuery.Exist()
				case selection.DoesNotExist:
					jsonQuery.NotExist()
				case selection.Equals, selection.DoubleEquals:
					jsonQuery.Equal(values[0])
				case selection.NotEquals:
					jsonQuery.NotEqual(values[0])
				case selection.In:
					jsonQuery.In(values...)
				case selection.NotIn:
					jsonQuery.NotIn(values...)
				default:
					continue
				}
				query = query.Where(jsonQuery)
			}
		}
	}

	if opts.ExtraLabelSelector != nil {
		if requirements, selectable := opts.ExtraLabelSelector.Requirements(); selectable {
			for _, require := range requirements {
				switch require.Key() {
				case SearchLabelFuzzyName:
					for _, name := range require.Values().List() {
						name = strings.TrimSpace(name)
						query = query.Where("name LIKE ?", fmt.Sprintf(`%%%s%%`, name))
					}
				}
			}
		}
	}

	if opts.EnhancedFieldSelector != nil {
		if requirements, selectable := opts.EnhancedFieldSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				var (
					fields      []string
					fieldErrors field.ErrorList
				)
				for _, f := range requirement.Fields() {
					if f.IsList() {
						fieldErrors = append(fieldErrors, field.Invalid(f.Path(), f.Name(), fmt.Sprintf("Storage<%s>: Not Support list field", StorageName)))
						continue
					}

					fields = append(fields, f.Name())
				}

				if len(fieldErrors) != 0 {
					return 0, nil, nil, apierrors.NewInvalid(schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"}, "fieldSelector", fieldErrors)
				}

				values := requirement.Values().List()
				jsonQuery := JSONQuery("object", fields...)
				switch requirement.Operator() {
				case selection.Exists:
					jsonQuery.Exist()
				case selection.DoesNotExist:
					jsonQuery.NotExist()
				case selection.Equals, selection.DoubleEquals:
					jsonQuery.Equal(values[0])
				case selection.NotEquals:
					jsonQuery.NotEqual(values[0])
				case selection.In:
					jsonQuery.In(values...)
				case selection.NotIn:
					jsonQuery.NotIn(values...)
				default:
					continue
				}
				query = query.Where(jsonQuery)
			}
		}
	}

	if applyFn != nil {
		var err error
		query, err = applyFn(query, opts)
		if err != nil {
			return 0, nil, nil, err
		}
	}

	var amount *int64
	if opts.WithRemainingCount != nil && *opts.WithRemainingCount {
		amount = new(int64)
		query = query.Count(amount)
	}

	// Due to performance reasons, the default order by is not set.
	// https://github.com/clusterpedia-io/clusterpedia/pull/44
	for _, orderby := range opts.OrderBy {
		orderByField := orderby.Field
		if orderByField == "resource_version" {
			orderByField = "CAST(resource_version as decimal)"
		}

		column := clause.OrderByColumn{
			Column: clause.Column{Name: orderByField, Raw: true},
			Desc:   orderby.Desc,
		}
		query = query.Order(column)

		// if orderby.Field is unsupported, return invalid error?
	}
	// kube ListOptions does not specify a limit default value of 0, gorm will execute limit = 0, resulting in the return of empty data.
	// https://github.com/go-gorm/gorm/commit/e8f48b5c155b6fbf2e1fe6a554e2280f62af21a7
	if opts.Limit > 0 {
		query = query.Limit(int(opts.Limit))
	}

	offset, err := strconv.Atoi(opts.Continue)
	if err == nil {
		query = query.Offset(offset)
	}
	return int64(offset), amount, query, nil
}
