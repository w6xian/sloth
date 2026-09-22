package ref

import (
	"context"
	"reflect"
)

var commonTypes = []string{"int", "int8", "int16", "int32", "int64", "uint", "uint16", "uint32", "uint64", "float32", "float64", "string", "uint8", "byte", "rune", "bool"}

// Precompute the reflect type for context.
var typeOfContext = reflect.TypeOf((*context.Context)(nil)).Elem()

// Precompute the reflect type for error.
var typeOfError = reflect.TypeOf((*error)(nil)).Elem()

type Functions []string
type ServiceApi map[string]FuncStruct

type ServiceFuncs struct {
	N string                    // name of service
	V reflect.Value             // receiver of methods for the service
	M map[string]reflect.Method // registered methods
	A ServiceApi                // arguments of methods
}

type FuncStruct struct {
	Name   string `json:"name"`
	Define string `json:"define"`
	// Args 业务入参的类型列表（按位置，不含首参 context.Context——它由框架注入）。
	//
	// 只保留 type 不带 name：reflect 只能拿到形参**类型**，Go 不保留形参名，
	// 硬凑名字（arg0/arg1）不如直接让它按位置表达——调用方传参本来也是位置参数。
	Args []string `json:"args"`
	// Returns 返回值类型列表，最后一个恒为 error（见签名约定）。
	Returns []string `json:"returns"`
}

type ArgStruct struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}
