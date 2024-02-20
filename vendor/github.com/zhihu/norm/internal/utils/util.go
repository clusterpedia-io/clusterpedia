package utils

import "fmt"

// WrapField wrap 字段, 使其符合 nebula 插入的习惯. 如给 string 添加引号
func WrapField(in interface{}) string {
	switch value := in.(type) {
	case string:
		return "'" + value + "'"
	default:
		return fmt.Sprint(value)
	}
}
