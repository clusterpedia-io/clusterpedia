/* Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License,
 * attached with Common Clause Condition 1.0, found in the LICENSES directory.
 */

package nebula_go

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/vesoft-inc/nebula-go/v2/nebula"
)

type ValueWrapper struct {
	value *nebula.Value
}

func (valueWrapper ValueWrapper) IsEmpty() bool {
	return valueWrapper.GetType() == "empty"
}

func (valueWrapper ValueWrapper) IsNull() bool {
	return valueWrapper.value.IsSetNVal()
}

func (valueWrapper ValueWrapper) IsBool() bool {
	return valueWrapper.value.IsSetBVal()
}

func (valueWrapper ValueWrapper) IsInt() bool {
	return valueWrapper.value.IsSetIVal()
}

func (valueWrapper ValueWrapper) IsFloat() bool {
	return valueWrapper.value.IsSetFVal()
}

func (valueWrapper ValueWrapper) IsString() bool {
	return valueWrapper.value.IsSetSVal()
}

func (valueWrapper ValueWrapper) IsTime() bool {
	return valueWrapper.value.IsSetTVal()
}

func (valueWrapper ValueWrapper) IsDate() bool {
	return valueWrapper.value.IsSetDVal()
}

func (valueWrapper ValueWrapper) IsDateTime() bool {
	return valueWrapper.value.IsSetDtVal()
}

func (valueWrapper ValueWrapper) IsList() bool {
	return valueWrapper.value.IsSetLVal()
}

func (valueWrapper ValueWrapper) IsSet() bool {
	return valueWrapper.value.IsSetUVal()
}

func (valueWrapper ValueWrapper) IsMap() bool {
	return valueWrapper.value.IsSetMVal()
}

func (valueWrapper ValueWrapper) IsVertex() bool {
	return valueWrapper.value.IsSetVVal()
}

func (valueWrapper ValueWrapper) IsEdge() bool {
	return valueWrapper.value.IsSetEVal()
}

func (valueWrapper ValueWrapper) IsPath() bool {
	return valueWrapper.value.IsSetPVal()
}

func (valueWrapper ValueWrapper) AsNull() (nebula.NullType, error) {
	if valueWrapper.value.IsSetNVal() {
		return valueWrapper.value.GetNVal(), nil
	}
	return -1, fmt.Errorf("Failed to convert value %s to Null", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsBool() (bool, error) {
	if valueWrapper.value.IsSetBVal() {
		return valueWrapper.value.GetBVal(), nil
	}
	return false, fmt.Errorf("Failed to convert value %s to bool", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsInt() (int64, error) {
	if valueWrapper.value.IsSetIVal() {
		return valueWrapper.value.GetIVal(), nil
	}
	return -1, fmt.Errorf("Failed to convert value %s to int", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsFloat() (float64, error) {
	if valueWrapper.value.IsSetFVal() {
		return valueWrapper.value.GetFVal(), nil
	}
	return -1, fmt.Errorf("Failed to convert value %s to float", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsString() (string, error) {
	if valueWrapper.value.IsSetSVal() {
		return string(valueWrapper.value.GetSVal()), nil
	}
	return "", fmt.Errorf("Failed to convert value %s to string", valueWrapper.GetType())
}

// TODO: Need to wrap TimeWrapper
func (valueWrapper ValueWrapper) AsTime() (*nebula.Time, error) {
	if valueWrapper.value.IsSetTVal() {
		return valueWrapper.value.GetTVal(), nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to Time", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsDate() (*nebula.Date, error) {
	if valueWrapper.value.IsSetDVal() {
		return valueWrapper.value.GetDVal(), nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to Date", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsDateTime() (*nebula.DateTime, error) {
	if valueWrapper.value.IsSetDtVal() {
		return valueWrapper.value.GetDtVal(), nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to DateTime", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsList() ([]ValueWrapper, error) {
	if valueWrapper.value.IsSetLVal() {
		var varList []ValueWrapper
		vals := valueWrapper.value.GetLVal().Values
		for _, val := range vals {
			varList = append(varList, ValueWrapper{val})
		}
		return varList, nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to List", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsDedupList() ([]ValueWrapper, error) {
	if valueWrapper.value.IsSetUVal() {
		var varList []ValueWrapper
		vals := valueWrapper.value.GetUVal().Values
		for _, val := range vals {
			varList = append(varList, ValueWrapper{val})
		}
		return varList, nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to set(deduped list)", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsMap() (map[string]ValueWrapper, error) {
	if valueWrapper.value.IsSetMVal() {
		newMap := make(map[string]ValueWrapper)

		kvs := valueWrapper.value.GetMVal().Kvs
		for key, val := range kvs {
			newMap[key] = ValueWrapper{val}
		}
		return newMap, nil
	}
	return nil, fmt.Errorf("Failed to convert value %s to Map", valueWrapper.GetType())
}

func (valueWrapper ValueWrapper) AsNode() (*Node, error) {
	if !valueWrapper.value.IsSetVVal() {
		return nil, fmt.Errorf("Failed to convert value %s to Node, value is not an vertex", valueWrapper.GetType())
	}
	vertex := valueWrapper.value.VVal
	node, err := genNode(vertex)
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (valueWrapper ValueWrapper) AsRelationship() (*Relationship, error) {
	if !valueWrapper.value.IsSetEVal() {
		return nil, fmt.Errorf("Failed to convert value %s to Relationship, value is not an edge", valueWrapper.GetType())
	}
	edge := valueWrapper.value.EVal
	relationship, err := genRelationship(edge)
	if err != nil {
		return nil, err
	}
	return relationship, nil
}

func (valueWrapper ValueWrapper) AsPath() (*PathWrapper, error) {
	if !valueWrapper.value.IsSetPVal() {
		return nil, fmt.Errorf("Failed to convert value %s to PathWrapper, value is not an edge", valueWrapper.GetType())
	}
	path, err := genPathWrapper(valueWrapper.value.PVal)
	if err != nil {
		return nil, err
	}
	return path, nil
}

// Returns the value type of value in the valueWrapper in string
func (valueWrapper ValueWrapper) GetType() string {
	if valueWrapper.value.IsSetNVal() {
		return "null"
	} else if valueWrapper.value.IsSetBVal() {
		return "bool"
	} else if valueWrapper.value.IsSetIVal() {
		return "int"
	} else if valueWrapper.value.IsSetFVal() {
		return "float"
	} else if valueWrapper.value.IsSetSVal() {
		return "string"
	} else if valueWrapper.value.IsSetDVal() {
		return "date"
	} else if valueWrapper.value.IsSetTVal() {
		return "time"
	} else if valueWrapper.value.IsSetDtVal() {
		return "datetime"
	} else if valueWrapper.value.IsSetVVal() {
		return "vertex"
	} else if valueWrapper.value.IsSetEVal() {
		return "edge"
	} else if valueWrapper.value.IsSetPVal() {
		return "path"
	} else if valueWrapper.value.IsSetLVal() {
		return "list"
	} else if valueWrapper.value.IsSetMVal() {
		return "map"
	} else if valueWrapper.value.IsSetUVal() {
		return "set"
	}
	return "empty"
}

// String() returns the value in the ValueWrapper as a string.
// Maps in the output will be sorted by key value in alphabetical order.
// For vetex, the output is in form (vid: tagName{propKey: propVal, propKey2, propVal2}),
// For edge, the output is in form (SrcVid)-[name]->(DstVid)@Ranking{prop1: val1, prop2: val2}
// where arrow direction depends on edgeType
// For path, the output is in form(v1)-[name@edgeRanking]->(v2)-[name@edgeRanking]->(v3)
func (valWarp ValueWrapper) String() string {
	value := valWarp.value
	if value.IsSetNVal() {
		return value.GetNVal().String()
	} else if value.IsSetBVal() {
		return fmt.Sprintf("%t", value.GetBVal())
	} else if value.IsSetIVal() {
		return fmt.Sprintf("%d", value.GetIVal())
	} else if value.IsSetFVal() {
		fStr := strconv.FormatFloat(value.GetFVal(), 'f', -1, 64)
		if !strings.Contains(fStr, ".") {
			fStr = fStr + ".0"
		}
		return fStr
	} else if value.IsSetSVal() {
		return `"` + string(value.GetSVal()) + `"`
	} else if value.IsSetDVal() { // Date yyyy-mm-dd
		date := value.GetDVal()
		return fmt.Sprintf("%d-%02d-%02d", date.Year, date.Month, date.Day)
	} else if value.IsSetTVal() { // Time HH:MM:SS.MS
		time := value.GetTVal()
		return fmt.Sprintf("%02d:%02d:%02d.%03d", time.Hour, time.Minute, time.Sec, time.Microsec)
	} else if value.IsSetDtVal() { // DateTime yyyy-mm-ddTHH:MM:SS.MS  TODO: add time zone
		dateTime := value.GetDtVal()
		return fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d.%03d",
			dateTime.Year, dateTime.Month, dateTime.Day,
			dateTime.Hour, dateTime.Minute, dateTime.Sec, dateTime.Microsec)
	} else if value.IsSetVVal() { // Vertex format: ("VertexID" :tag1{k0: v0,k1: v1}:tag2{k2: v2})
		vertex := value.GetVVal()
		node, _ := genNode(vertex)
		return node.String()
	} else if value.IsSetEVal() { // Edge format: [:edge src->dst @ranking {propKey1: propVal1}]
		edge := value.GetEVal()
		relationship, _ := genRelationship(edge)
		return relationship.String()
	} else if value.IsSetPVal() {
		// Path format: ("VertexID" :tag1{k0: v0,k1: v1})-[:TypeName@ranking {propKey1: propVal1}]->("VertexID2" :tag1{k0: v0,k1: v1} :tag2{k2: v2})-[:TypeName@ranking {propKey2: propVal2}]->("VertexID3" :tag1{k0: v0,k1: v1})
		path := value.GetPVal()
		pathWrap, _ := genPathWrapper(path)
		return pathWrap.String()
	} else if value.IsSetLVal() { // List
		lval := value.GetLVal()
		var strs []string
		for _, val := range lval.Values {
			strs = append(strs, ValueWrapper{val}.String())
		}
		return fmt.Sprintf("[%s]", strings.Join(strs, ", "))
	} else if value.IsSetMVal() { // Map
		// {k0: v0, k1: v1}
		mval := value.GetMVal()
		var keyList []string
		var output []string
		kvs := mval.Kvs
		for k := range kvs {
			keyList = append(keyList, k)
		}
		sort.Strings(keyList)
		for _, k := range keyList {
			output = append(output, fmt.Sprintf("%s: %s", k, ValueWrapper{kvs[k]}.String()))
		}
		return fmt.Sprintf("{%s}", strings.Join(output, ", "))
	} else if value.IsSetUVal() {
		// set to string
		uval := value.GetUVal()
		var strs []string
		for _, val := range uval.Values {
			strs = append(strs, ValueWrapper{val}.String())
		}
		return fmt.Sprintf("[%s]", strings.Join(strs, ", "))
	} else { // is empty
		return ""
	}
}
