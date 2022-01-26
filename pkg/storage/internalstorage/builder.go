package internalstorage

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JSONQueryExpression struct {
	column string
	keys   []string

	not    bool
	values []interface{}
}

func JSONQuery(column string, keys ...string) *JSONQueryExpression {
	return &JSONQueryExpression{column: column, keys: keys}
}

func (jsonQuery *JSONQueryExpression) Equal(value interface{}) *JSONQueryExpression {
	jsonQuery.values = []interface{}{value}
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) NotEqual(value interface{}) *JSONQueryExpression {
	jsonQuery.not = true
	jsonQuery.values = []interface{}{value}
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) In(values ...interface{}) *JSONQueryExpression {
	jsonQuery.values = values
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) NotIn(values ...interface{}) *JSONQueryExpression {
	jsonQuery.not = true
	jsonQuery.values = values
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) Build(builder clause.Builder) {
	if stmt, ok := builder.(*gorm.Statement); ok {
		if len(jsonQuery.keys) == 0 {
			return
		}

		switch stmt.Dialector.Name() {
		case "mysql", "sqlite":
			builder.WriteString("JSON_EXTRACT(" + stmt.Quote(jsonQuery.column) + ",")
			builder.AddVar(stmt, "$."+strings.Join(jsonQuery.keys, "."))

			switch len(jsonQuery.values) {
			case 0:
				builder.WriteString(") IS NOT NULL")
			case 1:
				if jsonQuery.not {
					builder.WriteString(") != ")
				} else {
					builder.WriteString(") = ")
				}

				if _, ok := jsonQuery.values[0].(bool); ok {
					builder.WriteString(fmt.Sprint(jsonQuery.values[0]))
				} else {
					builder.AddVar(builder, jsonQuery.values[0])
				}
			default:
				if jsonQuery.not {
					builder.WriteString(") NOT IN ")
				} else {
					builder.WriteString(") IN ")
				}
				builder.AddVar(builder, jsonQuery.values)
			}
		case "postgres":
			stmt.WriteQuoted(jsonQuery.column)
			for _, key := range jsonQuery.keys[0 : len(jsonQuery.keys)-1] {
				stmt.WriteString(" -> ")
				stmt.AddVar(builder, key)
			}

			switch len(jsonQuery.values) {
			case 0:
				stmt.WriteString(" ? ")
				stmt.AddVar(builder, jsonQuery.keys[len(jsonQuery.keys)-1])
			case 1:
				stmt.WriteString(" ->> ")
				stmt.AddVar(builder, jsonQuery.keys[len(jsonQuery.keys)-1])

				if jsonQuery.not {
					stmt.WriteString(" != ")
				} else {
					stmt.WriteString(" = ")
				}

				if _, ok := jsonQuery.values[0].(bool); ok {
					builder.WriteString(fmt.Sprint(jsonQuery.values[0]))
				} else {
					builder.AddVar(builder, jsonQuery.values[0])
				}
			default:
				stmt.WriteString(" ->> ")
				stmt.AddVar(builder, jsonQuery.keys[len(jsonQuery.keys)-1])

				if jsonQuery.not {
					builder.WriteString(" NOT IN ")
				} else {
					builder.WriteString(" IN ")
				}
				builder.AddVar(builder, jsonQuery.values)
			}
		}
	}
}

func buildParentOwner(db *gorm.DB, cluster, owner string, seniority int) interface{} {
	if seniority == 0 {
		return owner
	}

	parentOwner := buildParentOwner(db, cluster, owner, seniority-1)
	ownerQuery := db.Model(Resource{}).Select("uid").Where(map[string]interface{}{"cluster": cluster})
	if _, ok := parentOwner.(string); ok {
		return ownerQuery.Where("owner_uid = ?", parentOwner)
	}
	return ownerQuery.Where("owner_uid IN (?)", parentOwner)
}
