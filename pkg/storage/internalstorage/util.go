package internalstorage

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	internal "github.com/clusterpedia-io/api/clusterpedia"
)

const (
	SearchLabelFuzzyName = "internalstorage.clusterpedia.io/fuzzy-name"

	URLQueryWhereSQL = "whereSQL"
)

var (
	supportedOrderByFields = sets.NewString("cluster", "namespace", "name", "created_at", "resource_version")
)

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

	if utilfeature.DefaultMutableFeatureGate.Enabled(AllowRawSQLQuery) {
		if len(opts.URLQuery[URLQueryWhereSQL]) > 0 {
			// TODO: prevent SQL injection.
			// If a string of numbers is passed in from SQL, the query will be taken as ID by default.
			// If the SQL contains English letter, it will be passed in as column.
			query = query.Where(opts.URLQuery[URLQueryWhereSQL][0])
		}
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
		field := orderby.Field
		if supportedOrderByFields.Has(field) {
			if field == "resource_version" {
				field = "CAST(resource_version as decimal)"
			}
			column := clause.OrderByColumn{
				Column: clause.Column{Name: field, Raw: true},
				Desc:   orderby.Desc,
			}
			query = query.Order(column)
		}

		// if orderby.Field is unsupported, return invalid error?
	}

	if opts.Limit != -1 {
		query = query.Limit(int(opts.Limit))
	}

	offset, err := strconv.Atoi(opts.Continue)
	if err == nil {
		query = query.Offset(offset)
	}
	return int64(offset), amount, query, nil
}
