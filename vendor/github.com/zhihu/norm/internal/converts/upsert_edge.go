package converts

import (
	"reflect"
	"strings"
	"text/template"
)

type upsertEdgeStruct struct {
	Name       string
	Src, Dst   string
	UpdateProp string
}

var upsertEdgeTemplate = template.Must(template.New("upsert_edge").
	Parse("upsert edge on {{.Name}} {{.Src}} -> {{.Dst}} set {{.UpdateProp}} "))

// ConvertToUpsertEdgeSql 转换结构体为 Upsert 边的 sql
func ConvertToUpsertEdgeSql(in interface{}, edgeName string, src, dst string) (string, error) {
	switch values := in.(type) {
	case map[string]interface{}:
		return buildUpsertEdgeSql(values, edgeName, src, dst), nil
	case *map[string]interface{}:
		return buildUpsertEdgeSql(*values, edgeName, src, dst), nil
	default:
		tagMap, err := parseStructToMap(reflect.ValueOf(in), true)
		if err != nil {
			return "", err
		}
		return buildUpsertEdgeSql(tagMap, edgeName, src, dst), nil
	}
}

func buildUpsertEdgeSql(tagMap map[string]interface{}, edgeName string, src, dst string) string {
	buf := new(strings.Builder)
	upsertEdgeTemplate.Execute(buf, &upsertEdgeStruct{
		Name:       edgeName,
		Src:        src,
		Dst:        dst,
		UpdateProp: genUpsertUpdateProp(tagMap),
	})
	return buf.String()
}
