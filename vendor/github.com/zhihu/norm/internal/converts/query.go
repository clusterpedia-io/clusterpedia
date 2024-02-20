package converts

import (
	"reflect"

	"github.com/pkg/errors"
	"github.com/zhihu/norm/dialectors"
)

var (
	NilPointError       = errors.New("assignment to entry in nil map")
	RecordNotFoundError = errors.New("record not found")
)

// UnmarshalResultSet 解组 ResultSet 为传入的结构体
func UnmarshalResultSet(resultSet *dialectors.ResultSet, in interface{}) error {
	switch values := in.(type) {
	case map[string]interface{}:
		return toMap(values, resultSet)
	case *map[string]interface{}:
		return toMap(*values, resultSet)
	case *[]map[string]interface{}:
		return toMapSlice(values, resultSet)
	default:
		val := reflect.ValueOf(values)
		switch val.Kind() {
		case reflect.Ptr:
			val = reflect.Indirect(val)
			switch val.Kind() {
			case reflect.Struct:
				return toStruct(val, resultSet)
			case reflect.Slice:
				return toStructSlice(val, resultSet)
			case reflect.Int8, reflect.Int16, reflect.Int, reflect.Int32, reflect.Int64:
				return toInt(val, resultSet)
			default:
				return errors.Errorf("not support type. type is:%v", val.Kind())
			}
		default:
			return errors.New("must be ptr")
		}
	}
}

func toInt(val reflect.Value, resultSet *dialectors.ResultSet) (err error) {
	if val.Interface() == nil {
		return NilPointError
	}

	if resultSet.GetRowSize() < 1 {
		val.SetInt(0)
		return nil
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
	cnt := resultSet.GetRows()[0].GetValues()[0].IVal
	val.SetInt(*cnt)
	return nil
}

func toStruct(val reflect.Value, resultSet *dialectors.ResultSet) (err error) {
	if val.Interface() == nil {
		return NilPointError
	}

	if resultSet.GetRowSize() < 1 {
		return RecordNotFoundError
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

	row := resultSet.GetRows()[0]
	fieldTagMap := getStructFieldTagMap(val.Type())
	for j, col := range resultSet.GetColNames() {
		fieldPos, ok := fieldTagMap[col]
		if !ok {
			continue
		}
		value := row.GetValues()[j]
		field := val.Field(fieldPos)
		err = setFieldValue(col, field, value)
	}

	return
}

func toStructSlice(val reflect.Value, resultSet *dialectors.ResultSet) (err error) {
	if val.Interface() == nil {
		return NilPointError
	}
	if resultSet.GetRowSize() < 1 {
		return
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

	val.Set(reflect.MakeSlice(val.Type(), resultSet.GetRowSize(), resultSet.GetRowSize()))
	fieldTagMap := getStructFieldTagMap(val.Index(0).Type())
	for i, row := range resultSet.GetRows() {
		// 这里可以优化 GetColNames, 只循环两个共有的 key
		for j, col := range resultSet.GetColNames() {
			fieldPos, ok := fieldTagMap[col]
			if !ok {
				continue
			}
			nValue := row.GetValues()[j]
			field := val.Index(i).Field(fieldPos)
			err = setFieldValue(col, field, nValue)
		}
	}
	return
}

func toMap(values map[string]interface{}, resultSet *dialectors.ResultSet) error {
	if values == nil {
		return NilPointError
	}
	if resultSet.GetRowSize() < 1 {
		return RecordNotFoundError
	}
	row := resultSet.GetRows()[0]
	for i, col := range resultSet.GetColNames() {
		values[col] = nValueToInterface(row.Values[i])
	}
	return nil
}

func toMapSlice(values *[]map[string]interface{}, resultSet *dialectors.ResultSet) error {
	if values == nil {
		return NilPointError
	}

	cols := resultSet.GetColNames()
	_values := make([]map[string]interface{}, resultSet.GetRowSize())
	for i, row := range resultSet.GetRows() {
		_values[i] = make(map[string]interface{})
		for j, col := range cols {
			_values[i][col] = nValueToInterface(row.Values[j])
		}
	}
	*values = append(*values, _values...)
	return nil
}
