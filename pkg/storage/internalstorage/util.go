package internalstorage

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	pediainternal "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
)

var (
	defaultOrderByFields   = []string{"cluster", "name", "namespace", "created_at", "resource_version"}
	defaultOrderByFieldSet = sets.NewString(defaultOrderByFields...)
)

func applyListOptionsToQuery(query *gorm.DB, opts *pediainternal.ListOptions) (int64, *int64, *gorm.DB) {
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

	if opts.LabelSelector != nil {
		if requirements, selectable := opts.LabelSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				values := make([]interface{}, 0, len(requirement.Values()))
				for value := range requirement.Values() {
					values = append(values, value)
				}

				// wrap label key with `""`
				jsonQuery := JSONQuery("object", "metadata", "labels", fmt.Sprintf("\"%s\"", requirement.Key()))
				switch requirement.Operator() {
				case selection.Exists:
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

	if opts.FieldSelector != nil {
		for _, requirement := range opts.FieldSelector.Requirements() {
			jsonQuery := JSONQuery("object", strings.Split(requirement.Field, ".")...)

			switch requirement.Operator {
			case selection.NotEquals:
				jsonQuery.NotEqual(requirement.Value)
			case selection.Equals, selection.DoubleEquals:
				jsonQuery.Equal(requirement.Value)
			default:
				continue
			}
			query = query.Where(jsonQuery)
		}
	}

	var amount *int64
	if opts.WithRemainingCount != nil && *opts.WithRemainingCount {
		amount = new(int64)
		query = query.Count(amount)
	}

	ordered := sets.NewString()
	for _, orderby := range opts.OrderBy {
		if defaultOrderByFieldSet.Has(orderby.Field) {
			column := clause.OrderByColumn{
				Column: clause.Column{Name: orderby.Field, Raw: true},
				Desc:   orderby.Desc,
			}
			query = query.Order(column)
			ordered.Insert(orderby.Field)
		}
	}

	for _, field := range defaultOrderByFields {
		if ordered.Has(field) {
			continue
		}

		column := clause.OrderByColumn{
			Column: clause.Column{Name: field, Raw: true},
		}
		query = query.Order(column)
	}

	if opts.Limit != -1 {
		query = query.Limit(int(opts.Limit))
	}

	offset, err := strconv.Atoi(opts.Continue)
	if err == nil {
		query = query.Offset(offset)
	}
	return int64(offset), amount, query
}

func getNewItemFunc(listObj runtime.Object, v reflect.Value) func() runtime.Object {
	if _, isUnstructuredList := listObj.(*unstructured.UnstructuredList); isUnstructuredList {
		return func() runtime.Object {
			return &unstructured.Unstructured{Object: map[string]interface{}{}}
		}
	}

	elem := v.Type().Elem()
	return func() runtime.Object {
		return reflect.New(elem).Interface().(runtime.Object)
	}
}

func appendListItem(v reflect.Value, data []byte, codec runtime.Codec, newItemFunc func() runtime.Object) error {
	obj, _, err := codec.Decode(data, nil, newItemFunc())
	if err != nil {
		return err
	}
	v.Set(reflect.Append(v, reflect.ValueOf(obj).Elem()))
	return nil
}
