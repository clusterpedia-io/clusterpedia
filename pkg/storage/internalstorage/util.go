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

	internal "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia"
)

var (
	supportedOrderByFields = sets.NewString("cluster", "namespace", "name", "created_at", "resource_version")
)

func applyListOptionsToQuery(query *gorm.DB, opts *internal.ListOptions, applyFn func(query *gorm.DB, opts *internal.ListOptions) (*gorm.DB, error)) (int64, *gorm.DB, error) {
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
					return 0, nil, apierrors.NewInvalid(schema.GroupKind{Group: internal.GroupName, Kind: "ListOptions"}, "fieldSelector", fieldErrors)
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
			return 0, nil, err
		}
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
	return int64(offset), query, nil
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
