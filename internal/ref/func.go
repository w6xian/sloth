package ref

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/w6xian/sloth/v3/internal/utils"
	"github.com/w6xian/sloth/v3/internal/utils/array"
)

// Register 注册服务
// rcvr 服务实例
// @example
//
//	var service struct {
//		Name string
//	}
//
// ref.Register(&service)
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
	m, a := suitable_methods(getType)
	service.M = m
	service.A = a
	return service
}

func suitable_methods(typ reflect.Type) (map[string]reflect.Method, map[string]FuncStruct) {
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

func instance_params(params reflect.Type, data []byte) (reflect.Value, error) {
	isPtr := params.Kind() == reflect.Pointer
	structType := params
	if isPtr {
		structType = params.Elem()
	}
	nameStr := structType.String()
	if nameStr == "[]byte" || nameStr == "[]uint8" {
		if isPtr {
			return reflect.ValueOf(&data), nil
		}
		return reflect.ValueOf(data), nil
	} else if array.InArray(nameStr, commonTypes) {
		// 检查参数类型，根据参数类型进行转换（[]byte改成 “name“对应的类型）
		r := get_param_type(isPtr, nameStr, data)
		return r, nil
	} else {
		// 转换参数类型为reflect.Value
		if instance, cErr := new_instance_reflect(structType); cErr == nil {
			utils.Deserialize(data, instance.Interface())
			// 根据需要返回对应的类型
			if !isPtr {
				return instance.Elem(), nil
			}
			return instance, nil
		}
	}
	return reflect.Value{}, fmt.Errorf("unknown type: %s", params.String())
}

// 根据type生成新的实例
func new_instance_reflect(typ reflect.Type) (reflect.Value, error) {
	if typ == nil {
		return reflect.Value{}, fmt.Errorf("unknown type: %s", typ.Name())
	}
	instance := reflect.New(typ)
	return instance, nil
}

func CallFuncWithContext(ctx context.Context, Fns *ServiceFuncs, method string, args ...[]byte) ([]byte, error) {
	mtd, ok := Fns.M[method]
	if !ok {
		return nil, errors.New("method not found")
	}
	funcArgs := []reflect.Value{
		Fns.V,                // 需要第一个为方法所属对象，【必须】这个是反射参数要求
		reflect.ValueOf(ctx), // 这个是context.Context参数，是习惯传递第一个参数，不是反射参数要求
	}
	return call_instance_func(mtd, funcArgs, args...)
}

func call_instance_func(mtd reflect.Method, params []reflect.Value, args ...[]byte) ([]byte, error) {
	defArgsNum := len(params)
	// func f(ctx)
	rArgsLen := len(args)
	maxArgs := mtd.Type.NumIn() - defArgsNum
	if rArgsLen > maxArgs {
		return nil, fmt.Errorf("too many arguments: got %d, want at most %d", rArgsLen, maxArgs)
	}
	// Elem() 相当于 *T 取指针指向的类型
	// more args
	for i := range rArgsLen {
		data := args[i]
		inx := mtd.Type.In(i + defArgsNum)
		param, iErr := instance_params(inx, data)
		if iErr != nil {
			return nil, iErr
		}
		params = append(params, param)
	}
	ret := mtd.Func.Call(params)
	if len(ret) != 2 {
		return nil, fmt.Errorf("call func  error, expect 2 return values, but got %d", len(ret))
	}

	iErr, ok := ret[1].Interface().(error)
	if ok && iErr != nil {
		return nil, iErr
	}

	// 调用成功，返回结果
	data := ret[0].Interface()
	// 调用成功，返回结果
	resp, err := utils.AnyToBytes(data)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
