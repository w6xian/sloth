package sloth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/w6xian/sloth/v4/types/trpc"
)

// TestMetaFuncs 自省服务："_.Funcs" 返回 MCP tools/list 形态的方法清单。
func TestMetaFuncs(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		t.Fatalf("register err: %v", err)
	}

	raw, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: MetaFuncs})
	if err != nil {
		t.Fatalf("call _.Funcs err: %v", err)
	}
	var got FuncsResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal err: %v, raw=%s", err, raw)
	}

	// 三个业务方法（Hello / Add / Echo），自省服务自己不出现在清单里
	if len(got.Tools) != 3 {
		t.Fatalf("want 3 tools, got %d: %+v", len(got.Tools), got.Tools)
	}
	byName := make(map[string]FuncTool, len(got.Tools))
	for _, tool := range got.Tools {
		byName[tool.Name] = tool
	}
	if _, bad := byName[MetaFuncs]; bad {
		t.Fatal("meta service should not list itself")
	}
}

// TestMetaFuncsCaseSensitive 方法名精确匹配：写成小写的 "_.funcs" 必须报错，
// 不悄悄纠偏成大写（与其它服务方法的行为保持一致）。
func TestMetaFuncsCaseSensitive(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &echoService{}, "echo"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	_, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: "_.funcs"})
	if !errors.Is(err, ErrMethodNotFound) {
		t.Fatalf("err = %v, want ErrMethodNotFound", err)
	}
}

// ctxArgService 用来钉住"只忽略第一个 ctx"：第二个 context.Context 是业务参数，
// 必须出现在清单里（漏了调用方就会少传一个参数）。
type ctxArgService struct{}

func (s *ctxArgService) Take(ctx context.Context, data []byte, inner context.Context) ([]byte, error) {
	return data, nil
}

func TestCtxSkippedOnce(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("cb", &ctxArgService{}, ""); err != nil {
		t.Fatalf("register err: %v", err)
	}

	result := svr.Funcs()
	if len(result.Tools) != 1 {
		t.Fatalf("want 1 tool, got %+v", result.Tools)
	}
	// 三个入参里只有后两个是业务参数：ctx 跳过、inner 保留
	props := result.Tools[0].InputSchema.Properties
	if len(props) != 2 {
		t.Fatalf("properties = %+v, want arg0(data) + arg1(inner)", props)
	}
	if got := props["arg0"].Description; got != "[]uint8" {
		t.Fatalf("arg0 = %q, want []uint8", got)
	}
	if got := props["arg1"].Description; got != "context.Context" {
		t.Fatalf("arg1 = %q, want context.Context", got)
	}
}

// TestMetaFuncsSchema 清单里每个方法的 MCP 结构：入参、返回值、描述。
func TestMetaFuncsSchema(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	if err := svr.Register("v1", &echoService{}, "echo service"); err != nil {
		t.Fatalf("register err: %v", err)
	}

	raw, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: MetaFuncs})
	if err != nil {
		t.Fatalf("call _.Funcs err: %v", err)
	}
	var got FuncsResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal err: %v, raw=%s", err, raw)
	}
	byName := make(map[string]FuncTool, len(got.Tools))
	for _, tool := range got.Tools {
		byName[tool.Name] = tool
	}

	hello, ok := byName["v1.Hello"]
	if !ok {
		t.Fatalf("v1.Hello missing: %+v", got.Tools)
	}
	if hello.Description != "echo service" {
		t.Fatalf("description = %q, want the metadata of its service", hello.Description)
	}
	if hello.InputSchema.Type != "object" {
		t.Fatalf("inputSchema.type = %q", hello.InputSchema.Type)
	}
	wantArg := FuncType{Type: "string", Description: "string"}
	if got := hello.InputSchema.Properties["arg0"]; got != wantArg {
		t.Fatalf("arg0 = %+v, want %+v", got, wantArg)
	}
	if len(hello.InputSchema.Required) != 1 || hello.InputSchema.Required[0] != "arg0" {
		t.Fatalf("required = %+v", hello.InputSchema.Required)
	}
	if hello.OutputSchema == nil || hello.OutputSchema.Type != "array" {
		t.Fatalf("outputSchema = %+v", hello.OutputSchema)
	}
	// Hello 返回 (string, error)：位置元组 [string, string]，且 items:false 表示到此为止
	if len(hello.OutputSchema.PrefixItems) != 2 ||
		hello.OutputSchema.PrefixItems[0].Type != "string" ||
		hello.OutputSchema.PrefixItems[1].Description != "error" {
		t.Fatalf("prefixItems = %+v", hello.OutputSchema.PrefixItems)
	}
	if hello.OutputSchema.Items != false {
		t.Fatalf("items = %#v, want false (tuple)", hello.OutputSchema.Items)
	}

	// Add(ctx, a int, b int)：两个整数入参
	add := byName["v1.Add"]
	if len(add.InputSchema.Properties) != 2 || add.InputSchema.Properties["arg1"].Type != "integer" {
		t.Fatalf("v1.Add inputSchema = %+v", add.InputSchema)
	}
}

// TestJSONSchemaType Go 类型到 JSON Schema 类型的映射。
//
// "interface {}" 单独拎出来：它以前被 HasPrefix("int") 误判成 integer
// ——any 返回值的方法（很常见）schema 就是错的。
func TestJSONSchemaType(t *testing.T) {
	cases := map[string]string{
		"string":            "string",
		"[]byte":            "string",
		"[]uint8":           "string",
		"error":             "string",
		"time.Time":         "string",
		"bool":              "boolean",
		"int":               "integer",
		"int64":             "integer",
		"uint16":            "integer",
		"rune":              "integer",
		"float32":           "number",
		"float64":           "number",
		"[]string":          "array",
		"[]*main.AB":        "array",
		"map[string]int":    "object",
		"*types.AuthInfo":   "object", // 指针按它指向的类型看
		"interface {}":      "object",
		"any":               "object",
		"struct{}":          "object",
		"main.HelloService": "object",
	}
	for goType, want := range cases {
		if got := jsonSchemaType(goType); got != want {
			t.Fatalf("jsonSchemaType(%q) = %q, want %q", goType, got, want)
		}
	}
}

// TestMetaDataPerService 多个服务各自保留自己的描述（以前是单字段，注册第二个就被覆盖）。
func TestMetaDataPerService(t *testing.T) {
	svr := ServerConn(DefaultServer())
	defer svr.Close()
	for name, meta := range map[string]string{"a": "meta-a", "b": "meta-b"} {
		if err := svr.Register(name, &headerEchoService{}, meta); err != nil {
			t.Fatalf("register %s err: %v", name, err)
		}
	}

	call := func(mtd string) string {
		raw, err := svr.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{
			Method: mtd, Args: [][]byte{[]byte("v")},
		})
		if err != nil {
			t.Fatalf("call %s err: %v", mtd, err)
		}
		return string(raw)
	}
	for name, meta := range map[string]string{"a": "meta-a", "b": "meta-b"} {
		if got, want := call(name+".Echo"), meta+"||v"; got != want {
			t.Fatalf("%s.Echo meta = %q, want %q", name, got, want)
		}
	}
}

// TestMetaFuncsOnClient 客户端侧同样注册了自省服务：服务端可以读某个客户端的方法清单。
func TestMetaFuncsOnClient(t *testing.T) {
	cli := ClientConn(DefaultClient())
	defer cli.Close()
	if err := cli.Register("shop", &echoService{}, "client side service"); err != nil {
		t.Fatalf("register err: %v", err)
	}
	raw, err := cli.CallFunc(context.Background(), nil, nil, nil, &trpc.RpcCaller{Method: MetaFuncs})
	if err != nil {
		t.Fatalf("call _.funcs err: %v", err)
	}
	var got FuncsResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal err: %v, raw=%s", err, raw)
	}
	if len(got.Tools) != 3 || got.Tools[0].Name != "shop.Add" {
		t.Fatalf("unexpected client tools: %+v", got.Tools)
	}
	if got.Tools[0].Description != "client side service" {
		t.Fatalf("description = %q", got.Tools[0].Description)
	}
}
