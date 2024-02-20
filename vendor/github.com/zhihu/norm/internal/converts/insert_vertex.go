package converts

import (
	"errors"
	"reflect"
	"strings"
	"text/template"

	"github.com/zhihu/norm/constants"
	"github.com/zhihu/norm/internal/utils"
)

type createVertexStruct struct {
	Name         string
	Vid          string
	Keys, Values string
}

var createVertexTemplate = template.Must(template.New("insert_vertex").
	Parse("insert vertex {{.Name}}({{.Keys}}) values {{.Vid}}:({{.Values}})"))

// ConvertToCreateVertexSql 转换结构体为创建点的 sql
func ConvertToCreateVertexSql(in interface{}, tagName string, vidWithPolicy string) (string, error) {
	switch values := in.(type) {
	case map[string]interface{}:
		return buildCreateVertexSql(values, tagName, vidWithPolicy), nil
	case *map[string]interface{}:
		return buildCreateVertexSql(*values, tagName, vidWithPolicy), nil
	case []map[string]interface{}:
		return "", errors.New("batch insert not support now")
	case *[]map[string]interface{}:
		return "", errors.New("batch insert not support now")
	default:
		tagMap, err := parseStructToMap(reflect.ValueOf(in), true)
		if err != nil {
			return "", err
		}
		return buildCreateVertexSql(tagMap, tagName, vidWithPolicy), nil
	}
}

func buildCreateVertexSql(tagMap map[string]interface{}, tagName string, vidWithPolicy string) string {
	keysStr, ValuesStr := genInsertKVs(tagMap)

	buf := new(strings.Builder)
	createVertexTemplate.Execute(buf, &createVertexStruct{
		Name:   tagName,
		Vid:    vidWithPolicy,
		Keys:   keysStr,
		Values: ValuesStr,
	})
	return buf.String()
}

func genInsertKVs(tagMap map[string]interface{}) (string, string) {
	keys := make([]string, len(tagMap))
	values := make([]string, len(tagMap))
	i := 0
	for k, v := range tagMap {
		keys[i] = k
		values[i] = utils.WrapField(v)
		i++
	}
	keysStr := strings.Join(keys, ",")
	ValuesStr := strings.Join(values, ",")
	return keysStr, ValuesStr
}

// parseStructToMap 解析传入的 struct, 取指定 Tag 为key, 生成 map.
// TODO 可以优化为返回 keys, values, 然后考虑支持批量插入
func parseStructToMap(val reflect.Value, skipZero bool) (result map[string]interface{}, err error) {
	if val.Kind() == reflect.Ptr {
		return parseStructToMap(val.Elem(), skipZero)
	}

	if val.Kind() != reflect.Struct {
		return map[string]interface{}{}, errors.New("must be struct")
	}

	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				err = e
			} else {
				err = errors.New("unknown exec error")
			}
		}
	}()

	typ := val.Type()
	result = make(map[string]interface{})
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get(constants.StructTagName)
		if tag == "" {
			continue
		}
		value := val.Field(i)
		if _, ok := supportKind[value.Kind()]; !ok {
			continue
		}
		if skipZero && value.IsZero() {
			continue
		}
		result[tag] = value.Interface()
	}
	return
}

var supportKind = map[reflect.Kind]struct{}{
	reflect.Bool:    {},
	reflect.Int:     {},
	reflect.Int8:    {},
	reflect.Int16:   {},
	reflect.Int32:   {},
	reflect.Int64:   {},
	reflect.Float32: {},
	reflect.Float64: {},
	reflect.String:  {},
}
