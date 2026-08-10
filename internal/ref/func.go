package ref

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"strings"
)

func SuitableMethods(typ reflect.Type) (map[string]reflect.Method, map[string]FuncStruct) {
	methods := make(map[string]reflect.Method)
	// 方法 及定义的参数
	iface := make(map[string]FuncStruct)

	// 遍历所有方法
	for m := 0; m < typ.NumMethod(); m++ {
		m := typ.Method(m)
		// 这里可以加一些方法需要什么样的参数，比如第一个参数必须是context.Context
		if m.Type.NumIn() < 2 || m.Type.In(1) != reflect.TypeOf((*context.Context)(nil)).Elem() {
			continue
		}
		// Method must be exported.
		if m.PkgPath != "" {
			continue
		}
		if !m.IsExported() {
			continue
		}
		// 只限定第一个参数，一这是context.Context，后面的参数可以是任意类型
		if m.Type.NumIn() < 2 {
			log.Printf("[notice]method %s must have at least 1 arguments", m.Name)
			continue
		}
		arg1 := m.Type.In(1)
		// 判定第一个参数是不是context.Context
		if !arg1.Implements(typeOfContext) {
			log.Printf("[notice]method %s must have at least 1 arguments, first argument must be context.Context", m.Name)
			continue
		}
		// 返回值最后一个值需要是error
		if m.Type.NumOut() < 1 {
			log.Printf("[notice]method %s must have 1-2 return value and last return value must be error", m.Name)
			continue
		}
		if m.Type.NumOut() > 2 {
			log.Printf("[notice]method %s must have 1-2 return values and last return value must be error", m.Name)
			continue
		}
		out := m.Type.Out(m.Type.NumOut() - 1)
		if !out.Implements(typeOfError) {
			log.Printf("[notice]method %s must have at least 1 return value, last return value must be error", m.Name)
			continue
		}
		methods[m.Name] = m
		// 方法的参数
		args := make([]ArgStruct, 0)
		for i := 2; i < m.Type.NumIn(); i++ {
			args = append(args, ArgStruct{
				Name: fmt.Sprintf("arg%d", i-2),
				Type: m.Type.In(i).String(),
			})
		}
		s := strings.SplitN(m.Type.String(), ",", 2)
		api := fmt.Sprintf("%s(", m.Name)
		s[0] = api
		iface[m.Name] = FuncStruct{
			Name:   m.Name,
			Define: fmt.Sprintf("%s", strings.Join(s, "")),
		}
	}

	for _, m := range methods {
		log.Printf("[success]method %s is registered", m.Name)
	}

	return methods, iface
}

func Register(rcvr any) *ServiceFuncs {
	service := new(ServiceFuncs)
	getType := reflect.TypeOf(rcvr)
	service.V = reflect.ValueOf(rcvr)
	k := getType.Kind()
	if k == reflect.Pointer {
		el := getType.Elem()
		sname := fmt.Sprintf("%s.%s", el.PkgPath(), el.Name())
		service.N = sname
	} else {
		sname := fmt.Sprintf("%s.%s", getType.PkgPath(), getType.Name())
		service.N = sname
	}
	// Install the methods
	m, a := SuitableMethods(getType)
	service.M = m
	service.A = a
	return service
}
