package internalstorage

import (
	"fmt"
	"reflect"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"

	pediainternal "github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
)

var (
	supportedOrderByFields = sets.NewString("cluster", "namespace", "name", "created_at", "resource_version")
)

func applyListOptionsToQuery(query *gorm.DB, opts *pediainternal.ListOptions) (int64, *int64, *gorm.DB, error) {
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

	if opts.EnhancedFieldSelector != nil {
		if requirements, selectable := opts.EnhancedFieldSelector.Requirements(); selectable {
			for _, requirement := range requirements {
				values := make([]interface{}, 0, len(requirement.Values()))
				for value := range requirement.Values() {
					values = append(values, value)
				}

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
					return 0, nil, nil, apierrors.NewInvalid(schema.GroupKind{Group: pediainternal.GroupName, Kind: "ListOptions"}, "fieldSelector", fieldErrors)
				}

				jsonQuery := JSONQuery("object", fields...)
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

	var amount *int64
	if opts.WithRemainingCount != nil && *opts.WithRemainingCount {
		amount = new(int64)
		query = query.Count(amount)
	}

	// Due to performance reasons, the default order by is not set.
	// https://github.com/clusterpedia-io/clusterpedia/pull/44
	for _, orderby := range opts.OrderBy {
		if supportedOrderByFields.Has(orderby.Field) {
			column := clause.OrderByColumn{
				Column: clause.Column{Name: orderby.Field, Raw: true},
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
