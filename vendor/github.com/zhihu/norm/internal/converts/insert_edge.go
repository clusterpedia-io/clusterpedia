package converts

import (
	"errors"
	"reflect"
	"strings"
	"text/template"
)

type createEdgeStruct struct {
	Name         string
	Src, Dst     string
	Keys, Values string
}

var createEdgeTemplate = template.Must(template.New("insert_edge").
	Parse("insert edge {{.Name}}({{.Keys}}) values {{.Src}} -> {{.Dst}}:({{.Values}})"))

// ConvertToCreateEdgeSql 转换结构体为创建边的 sql
func ConvertToCreateEdgeSql(in interface{}, edgeName string, src, dst string) (string, error) {
	switch values := in.(type) {
	case map[string]interface{}:
		return buildCreateEdgeSql(values, edgeName, src, dst), nil
	case *map[string]interface{}:
		return buildCreateEdgeSql(*values, edgeName, src, dst), nil
	case []map[string]interface{}:
		return "", errors.New("batch insert not support now")
	case *[]map[string]interface{}:
		return "", errors.New("batch insert not support now")
	default:
		tagMap, err := parseStructToMap(reflect.ValueOf(in), true)
		if err != nil {
			return "", err
		}
		return buildCreateEdgeSql(tagMap, edgeName, src, dst), nil
	}
}

func buildCreateEdgeSql(tagMap map[string]interface{}, edgeName string, src, dst string) string {
	keysStr, ValuesStr := genInsertKVs(tagMap)

	buf := new(strings.Builder)
	createEdgeTemplate.Execute(buf, &createEdgeStruct{
		Name:   edgeName,
		Src:    src,
		Dst:    dst,
		Keys:   keysStr,
		Values: ValuesStr,
	})
	return buf.String()
}
