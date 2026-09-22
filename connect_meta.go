package sloth

import (
	"context"
	"sort"
	"strconv"
	"strings"
)

// MetaService 自省服务的固定服务名；读方法清单的调用形式是 "_.Funcs"。
//
// 约定：每个 *Connect 实例（服务端 ServerConn、客户端 ClientConn 都一样）在建
// 实例时就自动注册这个服务，所以对端随时可以读这一侧注册了哪些服务、每个方法
// 的入参和返回值是什么。双向都成立：
//   - client.Call(ctx, hdr, "_.Funcs")       读服务端清单
//   - server.Call(ctx, userId, "_.Funcs")    读某个客户端清单
//
// 方法名**大小写敏感、精确匹配**：它跟其它服务一样是 Go 的导出方法，写 "_.funcs"
// 只会拿到 ErrMethodNotFound —— 与其它方法的行为保持一致，不做额外纠偏。
//
// 服务名选 "_"：短，且与业务服务名几乎不可能冲突。业务自己注册 "_" 会拿到
// "service _ already registered"——内置实现优先，避免同一份清单一地两义。
const MetaService = "_"

// MetaFuncs 是自省方法的**完整方法名**（服务名 + 方法名），方便调用方引用。
const MetaFuncs = "_.Funcs"

// metaService 自省服务实现。
//
// 它自身不持有清单：数据来自所属 *Connect 的 serviceMap，所以每次调用读到的都
// 是当前注册表（Register 之后立刻可见），不需要额外同步。
type metaService struct {
	c *Connect
}

// Funcs 返回本端已注册方法的清单（MCP tools/list 形态），对端以 "_.Funcs" 调用。
//
// 清单里**不含自省服务自己**：它的用途是"照着它拼 RPC"，把 _.Funcs 也列进去
// 只会让调用方误递归；需要它的人已经知道它在。
func (m *metaService) Funcs(ctx context.Context) (any, error) {
	return m.c.Funcs(), nil
}

// Funcs 导出本端已注册服务的**业务方法**清单（MCP tools/list 形态）。
//
// 排序后再返回：serviceMap 是 map，遍历顺序不定，不排序的话同一份注册表每次
// 返回的 tools 顺序都在变（diff/比对时全是噪音）。
func (c *Connect) Funcs() FuncsResult {
	c.serviceMapMu.RLock()
	defer c.serviceMapMu.RUnlock()

	out := FuncsResult{Tools: make([]FuncTool, 0, 8)}
	for name, fn := range c.serviceMap {
		if name == MetaService {
			continue
		}
		for _, api := range fn.A {
			out.Tools = append(out.Tools, FuncTool{
				// MCP 里 tool name 要求唯一，这里用 "服务名.方法名" 天然不撞
				Name:         name + "." + api.Name,
				Description:  c.metaData[name],
				InputSchema:  inputSchema(api.Args),
				OutputSchema: outputSchema(api.Returns),
			})
		}
	}
	sort.Slice(out.Tools, func(i, j int) bool { return out.Tools[i].Name < out.Tools[j].Name })
	return out
}

// FuncsResult 是 "_.Funcs" 的返回值，对齐 MCP tools/list 的应答结构，
// 拿到之后可以直接喂给 MCP client。
type FuncsResult struct {
	Tools []FuncTool `json:"tools"`
}

// FuncTool 一个注册方法对应一个 tool。
type FuncTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// InputSchema 入参：RPC 传的是位置参数，所以 properties 的键是 arg0/arg1…
	InputSchema  FuncSchema  `json:"inputSchema"`
	OutputSchema *FuncSchema `json:"outputSchema,omitempty"`
}

// FuncSchema 够用就好的 JSON Schema 子集。
//
// 两处非典型用法：入参/返回值都是**位置**参数，所以都用元组形式表达——
// 入参用 properties(argN)+required（MCP client 只认 object），
// 返回值用 prefixItems + items:false（多返回值本质上是元组）。
type FuncSchema struct {
	Type        string              `json:"type"`
	Properties  map[string]FuncType `json:"properties,omitempty"`
	Required    []string            `json:"required,omitempty"`
	PrefixItems []FuncType          `json:"prefixItems,omitempty"`
	// Items 只在输出侧设为 false，表示"元组到此为止"（用 any 是为了让它值为 false 时也输出）
	Items any `json:"items,omitempty"`
}

// FuncType JSON Schema 的基本类型。
//
// Description 放的是**Go 类型名**：JSON Schema 只能表达 string/integer 这类基本
// 类型，而调用方真正需要知道的是 []byte 还是 *types.AuthInfo，这个信息丢不得。
type FuncType struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

func inputSchema(args []string) FuncSchema {
	props := make(map[string]FuncType, len(args))
	required := make([]string, 0, len(args))
	for i, typ := range args {
		name := argName(i)
		props[name] = schemaType(typ)
		required = append(required, name)
	}
	return FuncSchema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

func outputSchema(returns []string) *FuncSchema {
	if len(returns) == 0 {
		return nil
	}
	items := make([]FuncType, 0, len(returns))
	for _, typ := range returns {
		items = append(items, schemaType(typ))
	}
	return &FuncSchema{
		Type:        "array",
		PrefixItems: items,
		Items:       false,
	}
}

// argName 位置参数的键名：arg0 / arg1…
func argName(i int) string {
	return "arg" + strconv.Itoa(i)
}

// schemaType Go 类型 -> JSON Schema 类型。
func schemaType(goType string) FuncType {
	return FuncType{Type: jsonSchemaType(goType), Description: goType}
}

func jsonSchemaType(goType string) string {
	// 指针统一按它指向的类型看：*AB 在调用方手里就是一个对象
	t := strings.TrimPrefix(goType, "*")
	switch t {
	case "string", "[]byte", "[]uint8", "error", "time.Time", "time.Duration":
		return "string"
	case "bool":
		return "boolean"
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune":
		return "integer"
	case "float32", "float64":
		return "number"
	}
	switch {
	case strings.HasPrefix(t, "["):
		return "array"
	case strings.HasPrefix(t, "map["), strings.HasPrefix(t, "interface"),
		strings.HasPrefix(t, "struct"), strings.HasPrefix(t, "func("), t == "any":
		return "object"
	}
	// int8 / uint64 / float32 这类"前缀 + 位宽"不能只按前缀匹配：
	// "interface {}" 同样以 int 开头，直接 HasPrefix("int") 会把它误判成 integer。
	if n := strings.TrimPrefix(t, "uint"); n != t && isDigits(n) {
		return "integer"
	}
	if n := strings.TrimPrefix(t, "int"); n != t && isDigits(n) {
		return "integer"
	}
	if n := strings.TrimPrefix(t, "float"); n != t && isDigits(n) {
		return "number"
	}
	// 结构体 / 接口等都按对象处理，真实类型名在 description 里
	return "object"
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
