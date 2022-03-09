package internalstorage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type JSONQueryExpression struct {
	column string
	keys   []string

	not    bool
	values []string
}

func JSONQuery(column string, keys ...string) *JSONQueryExpression {
	return &JSONQueryExpression{column: column, keys: keys}
}

func (jsonQuery *JSONQueryExpression) Exist() *JSONQueryExpression {
	jsonQuery.not = false
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) NotExist() *JSONQueryExpression {
	jsonQuery.not = true
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) Equal(value string) *JSONQueryExpression {
	jsonQuery.not, jsonQuery.values = false, []string{value}
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) NotEqual(value string) *JSONQueryExpression {
	jsonQuery.not, jsonQuery.values = true, []string{value}
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) In(values ...string) *JSONQueryExpression {
	jsonQuery.not, jsonQuery.values = false, values
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) NotIn(values ...string) *JSONQueryExpression {
	jsonQuery.not, jsonQuery.values = true, values
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) Build(builder clause.Builder) {
	if stmt, ok := builder.(*gorm.Statement); ok {
		if len(jsonQuery.keys) == 0 {
			return
		}

		switch stmt.Dialector.Name() {
		case "mysql", "sqlite":
			writeString(builder, "JSON_UNQUOTE(JSON_EXTRACT("+stmt.Quote(jsonQuery.column)+",")
			builder.AddVar(stmt, fmt.Sprintf(`$."%s"`, strings.Join(jsonQuery.keys, `"."`)))
			writeString(builder, "))")

			switch len(jsonQuery.values) {
			case 0:
				if jsonQuery.not {
					writeString(builder, " IS NULL")
				} else {
					writeString(builder, " IS NOT NULL")
				}
			case 1:
				if jsonQuery.not {
					writeString(builder, " != ")
				} else {
					writeString(builder, " = ")
				}
				builder.AddVar(builder, jsonQuery.values[0])
			default:
				if jsonQuery.not {
					writeString(builder, " NOT IN ")
				} else {
					writeString(builder, " IN ")
				}
				builder.AddVar(builder, jsonQuery.values)
			}
		case "postgres":
			stmt.WriteQuoted(jsonQuery.column)
			for _, key := range jsonQuery.keys[0 : len(jsonQuery.keys)-1] {
				writeString(stmt, " -> ")
				stmt.AddVar(builder, key)
			}
			writeString(stmt, " ->> ")
			stmt.AddVar(builder, jsonQuery.keys[len(jsonQuery.keys)-1])

			switch len(jsonQuery.values) {
			case 0:
				if jsonQuery.not {
					writeString(stmt, " IS NULL")
				} else {
					writeString(stmt, " IS NOT NULL")
				}
			case 1:
				if jsonQuery.not {
					writeString(stmt, " != ")
				} else {
					writeString(stmt, " = ")
				}
				builder.AddVar(builder, jsonQuery.values[0])
			default:
				if jsonQuery.not {
					writeString(builder, " NOT IN ")
				} else {
					writeString(builder, " IN ")
				}
				builder.AddVar(builder, jsonQuery.values)
			}
		}
	}
}

func writeString(builder clause.Writer, str string) {
	_, _ = builder.WriteString(str)
}

func buildOwnerQueryByUID(db *gorm.DB, cluster, uid string, seniority int) interface{} {
	if seniority == 0 {
		return uid
	}

	parentOwner := buildOwnerQueryByUID(db, cluster, uid, seniority-1)
	ownerQuery := db.Model(Resource{}).Select("uid").Where(map[string]interface{}{"cluster": cluster})
	if _, ok := parentOwner.(string); ok {
		return ownerQuery.Where("owner_uid = ?", parentOwner)
	}
	return ownerQuery.Where("owner_uid IN (?)", parentOwner)
}

func buildOwnerQueryByName(db *gorm.DB, cluster string, namespaces []string, groupResource schema.GroupResource, name string, seniority int) interface{} {
	ownerQuery := db.Model(Resource{}).Select("uid").Where(map[string]interface{}{"cluster": cluster})
	if seniority != 0 {
		parentOwner := buildOwnerQueryByName(db, cluster, namespaces, groupResource, name, seniority-1)
		return ownerQuery.Where("owner_uid IN (?)", parentOwner)
	}

	if !groupResource.Empty() {
		ownerQuery = ownerQuery.Where(map[string]interface{}{"group": groupResource.Group, "resource": groupResource.Resource})
	}
	switch len(namespaces) {
	case 0:
	case 1:
		ownerQuery = ownerQuery.Where("namespace = ?", namespaces[0])
	default:
		ownerQuery = ownerQuery.Where("namespace IN (?)", namespaces)
	}
	return ownerQuery.Where("name = ?", name)
}
