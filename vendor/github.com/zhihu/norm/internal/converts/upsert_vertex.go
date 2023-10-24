package converts

import (
	"fmt"
	"reflect"
	"strings"
	"text/template"

	"github.com/zhihu/norm/internal/utils"
)

type upsertVertexStruct struct {
	Name       string
	Vid        string
	UpdateProp string
}

var upsertVertexTemplate = template.Must(template.New("upsert_vertex").
	Parse("upsert vertex on {{.Name}} {{.Vid}}) set {{.UpdateProp}}"))

// ConvertToUpsertVertexSql 转换结构体为更新点的 sql
func ConvertToUpsertVertexSql(in interface{}, tagName string, vidWithPolicy string) (string, error) {
	switch values := in.(type) {
	case map[string]interface{}:
		return buildUpsertVertexSql(values, tagName, vidWithPolicy), nil
	case *map[string]interface{}:
		return buildUpsertVertexSql(*values, tagName, vidWithPolicy), nil
	default:
		tagMap, err := parseStructToMap(reflect.ValueOf(in), true)
		if err != nil {
			return "", err
		}
		return buildUpsertVertexSql(tagMap, tagName, vidWithPolicy), nil
	}
}

func buildUpsertVertexSql(tagMap map[string]interface{}, tagName string, vidWithPolicy string) string {
	buf := new(strings.Builder)
	upsertVertexTemplate.Execute(buf, &upsertVertexStruct{
		Name:       tagName,
		Vid:        vidWithPolicy,
		UpdateProp: genUpsertUpdateProp(tagMap),
	})
	return buf.String()
}

func genUpsertUpdateProp(tagMap map[string]interface{}) string {
	updateProps := make([]string, len(tagMap))
	i := 0
	for k, v := range tagMap {
		updateProps[i] = fmt.Sprintf("%s=%s", k, utils.WrapField(v))
		i++
	}
	return strings.Join(updateProps, ",")
}
