package ref

import (
	"context"
	"reflect"
)

var commonTypes = []string{"int", "int32", "int64", "uint", "uint32", "uint64", "float32", "float64", "string", "uint8", "bool"}

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
}

type ArgStruct struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Desc string `json:"desc"`
}
