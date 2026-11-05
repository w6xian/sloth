package main

// dual-role：验证 sloth v4 能否在**同一个进程**里同时持有 server 与 client。
//
// 拓扑对应 RCP AI 的私域侧（已按新命名：ai-proxy / ai-agent / ai-rag）：
//
//	ai-proxy（云端，纯 server，:9101）
//	    ↑  agent 作为 client 拨号上来，并把自己注册成 "agent" 服务
//	ai-agent（私域，双角色）
//	    ├─ 对上游：client（上面那条）
//	    └─ 对下游：server（:9102）
//	          ↑  rag 作为 client 拨号上来，注册成 "rag" 服务
//
// 验证三条调用，缺任何一条都不能说"双角色可行"：
//
//	① 下行 proxy → agent.Ask    agent 是 client，被上游反向调用
//	② 下行 agent → rag.Search   同一个进程，这次它扮演 server
//	③ 上行 agent → v1.Chunk     agent 作为 client 调上游的方法（流式回传要走这条）
//
// 运行：
//
//	go run ./examples/dual-role
//
// 关键结论见文末打印的 PASS/FAIL 汇总。

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/slots"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/tlv"
)

const (
	proxyAddr = "localhost:9101" // ai-proxy（云端）
	agentAddr = "localhost:9102" // ai-agent 对内暴露的端口（私域）
)

// 调用超时：连不上/路由不通时不要吊死，快速失败好定位。
const callTimeout = 5 * time.Second

func main() {
	// 库自身日志只留错误，验证结果用 fmt 打，避免被握手日志淹没。
	sloth.SetLogLevel("error")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ---------- ① 云端 ai-proxy：纯 server ----------
	proxySmap := sloth.NewSMap()
	proxy := sloth.DefaultServer() // 注意：返回的是 *ClientRpc（命名反直觉，见源码注释）
	proxyConn := sloth.ServerConn(proxy, sloth.WithConnectProxy(router(proxySmap)))
	if err := proxyConn.Register("v1", &GatewayService{smap: proxySmap, tag: "proxy"}, "proxy"); err != nil {
		fatal("proxy register: %v", err)
	}
	if err := proxyConn.Listen(ctx, "ws", proxyAddr,
		option.WithOrigin("*"),
		option.WithServerHandleMessage(&ServerHandler{})); err != nil {
		fatal("proxy listen: %v", err)
	}
	go func() {
		if err := proxyConn.Serve(); err != nil {
			sloth.Errorw(ctx, "proxy serve exited", "err", err)
		}
	}()

	// ---------- ② 私域 ai-agent：双角色 ----------
	// 2a 对上游：client 角色。Register 的是"我提供的服务"，供云端反向调用。
	agentClient := sloth.DefaultClient() // *ServerRpc
	agentCliConn := sloth.ClientConn(agentClient)
	if err := agentCliConn.Register("agent", &AgentService{}, "agent"); err != nil {
		fatal("agent register(client): %v", err)
	}
	go func() {
		if err := agentCliConn.Dial(ctx, "ws", proxyAddr,
			option.WithClientHandleMessage(&ClientHandler{})); err != nil {
			sloth.Errorw(ctx, "agent dial exited", "err", err)
		}
	}()

	// 2b 对下游：server 角色。与 2a 是**两个独立的 sloth 实例**，互不干扰。
	agentSmap := sloth.NewSMap()
	agentServer := sloth.DefaultServer()
	agentSrvConn := sloth.ServerConn(agentServer, sloth.WithConnectProxy(router(agentSmap)))
	if err := agentSrvConn.Register("v1", &GatewayService{smap: agentSmap, tag: "agent"}, "agent-local"); err != nil {
		fatal("agent register(server): %v", err)
	}
	if err := agentSrvConn.Listen(ctx, "ws", agentAddr,
		option.WithOrigin("*"),
		option.WithServerHandleMessage(&ServerHandler{})); err != nil {
		fatal("agent listen: %v", err)
	}
	go func() {
		if err := agentSrvConn.Serve(); err != nil {
			sloth.Errorw(ctx, "agent serve exited", "err", err)
		}
	}()

	// ---------- ③ 私域 ai-rag：纯 client ----------
	ragClient := sloth.DefaultClient()
	ragConn := sloth.ClientConn(ragClient)
	if err := ragConn.Register("rag", &RagService{}, "rag"); err != nil {
		fatal("rag register: %v", err)
	}
	go func() {
		if err := ragConn.Dial(ctx, "ws", agentAddr,
			option.WithClientHandleMessage(&ClientHandler{})); err != nil {
			sloth.Errorw(ctx, "rag dial exited", "err", err)
		}
	}()

	// ---------- ④ 等连接就绪，并把 client 侧服务登记到对端 ----------
	// Reg 会由服务端分配一个负数 userId 并存进 SMap，之后上游才能按 userId 调下来。
	waitReg(ctx, "agent → proxy", agentClient, "agent", func() bool { _, ok := proxySmap.Get("agent"); return ok })
	waitReg(ctx, "rag → agent", ragClient, "rag", func() bool { _, ok := agentSmap.Get("rag"); return ok })

	// ---------- ⑤ 三条验证 ----------
	pass := map[string]bool{}

	// ① 下行：proxy 主动调 agent.Ask
	pass["① proxy → agent.Ask"] = step("① 下行 proxy → agent.Ask", func() (string, error) {
		uid, ok := proxySmap.Get("agent")
		if !ok {
			return "", fmt.Errorf("agent 未登记到 proxy 的 SMap")
		}
		data, err := proxy.Call(ctx, uid, "agent.Ask", []byte(`{"q":"查库存"}`))
		if err != nil {
			return "", err
		}
		return string(tlv.Value(data)), nil
	})

	// ② 下行：agent（server 角色）调 rag.Search
	pass["② agent → rag.Search"] = step("② 下行 agent → rag.Search", func() (string, error) {
		uid, ok := agentSmap.Get("rag")
		if !ok {
			return "", fmt.Errorf("rag 未登记到 agent 的 SMap")
		}
		data, err := agentServer.Call(ctx, uid, "rag.Search", []byte(`{"q":"退换货政策"}`))
		if err != nil {
			return "", err
		}
		return string(tlv.Value(data)), nil
	})

	// ③ 上行：agent（client 角色）调 proxy 的方法（流式 chunk 回传走这条）
	pass["③ agent → v1.Chunk"] = step("③ 上行 agent → v1.Chunk", func() (string, error) {
		data, err := agentClient.Call(ctx, "v1.Chunk", []byte(`{"seq":1,"delta":"答案片段"}`))
		if err != nil {
			return "", err
		}
		return string(tlv.Value(data)), nil
	})

	fmt.Println("\n================ 汇总 ================")
	allOK := true
	for _, name := range []string{"① proxy → agent.Ask", "② agent → rag.Search", "③ agent → v1.Chunk"} {
		st := "FAIL"
		if pass[name] {
			st = "PASS"
		} else {
			allOK = false
		}
		fmt.Printf("  %-28s %s\n", name, st)
	}
	if allOK {
		fmt.Println("\n结论：sloth v4 支持 server/client 同体（双角色），两个方向的调用都通。")
	} else {
		fmt.Println("\n结论：双角色存在不成立的调用方向，见上面 FAIL 项。")
		os.Exit(1)
	}
}

// router 把"本地没注册的服务名"解析成它所在连接的 userId。
// 客户端调另一个客户端注册的服务时走这条（agent 调 rag.Search 若不经 SMap 直连，就靠它）。
func router(smap *sloth.SMap) func(context.Context, string) (int64, error) {
	return func(ctx context.Context, service string) (int64, error) {
		node, err := sloth.GetNode(service)
		if err != nil {
			return 0, err
		}
		svrId, ok := smap.Get(node.Service)
		if !ok {
			return 0, fmt.Errorf("service %s not registered", node.Service)
		}
		return svrId, nil
	}
}

// waitReg 轮询调用对端的 v1.Reg 直到成功（Dial 是异步的，连上才能调）。
func waitReg(ctx context.Context, label string, cli *sloth.ServerRpc, name string, done func() bool) {
	for i := 0; i < 40; i++ {
		if done() {
			fmt.Printf("[ready] %s 已登记 %s\n", label, name)
			return
		}
		c, cancel := context.WithTimeout(ctx, callTimeout)
		_, err := cli.Call(c, "v1.Reg", name)
		cancel()
		if err == nil {
			// 调用成功不代表 SMap 已写入（服务端同步写），下一轮 done() 会确认
			time.Sleep(200 * time.Millisecond)
			continue
		}
		time.Sleep(250 * time.Millisecond)
	}
	fatal("%s 注册超时：%s 始终未登记", label, name)
}

func step(label string, fn func() (string, error)) bool {
	c, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_ = c
	out, err := fn()
	if err != nil {
		fmt.Printf("\n%s\n  FAIL: %v\n", label, err)
		return false
	}
	fmt.Printf("\n%s\n  OK  : %s\n", label, out)
	return true
}

func fatal(format string, args ...any) {
	fmt.Printf("FATAL: "+format+"\n", args...)
	os.Exit(1)
}

// ---------------------------------------------------------------- 服务实现

// GatewayService 是 server 侧的公共服务：Reg 让连上来的 client 把自己登记成
// 服务提供者，Chunk 供 client 上行调用（对应流式回传）。
type GatewayService struct {
	smap *sloth.SMap
	tag  string
}

// Reg 分配一个负数 userId 并把连接放进 bucket，之后上游按该 userId 调下来。
func (h *GatewayService) Reg(ctx context.Context, name string) ([]byte, error) {
	ch, ok := ctx.Value(sloth.ChannelKey).(bucket.IChannel)
	if !ok {
		return nil, fmt.Errorf("channel not found")
	}
	svr, ok := ctx.Value(sloth.BucketKey).(types.IBucket)
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	svrId, err := h.smap.Reg(name, false)
	if err != nil {
		return nil, err
	}
	ai := auth.AuthInfo{UserId: svrId, RoomId: -1, Token: "token_" + name, Ts: time.Now().Unix()}
	svr.Bucket(ai.UserId).Put(ai.UserId, ai.RoomId, ai.Token, ch)
	fmt.Printf("[%s] 登记服务 %s -> userId=%d\n", h.tag, name, svrId)
	return tlv.Json(ai), nil
}

// Chunk 模拟云端接收私域推上来的流式片段。
func (h *GatewayService) Chunk(ctx context.Context, delta []byte) ([]byte, error) {
	fmt.Printf("[%s] 收到上行 chunk: %s\n", h.tag, string(delta))
	return []byte(`{"ok":true,"from":"` + h.tag + `"}`), nil
}

// AgentService 是 ai-agent 作为 client 注册上去、供云端反向调用的服务。
type AgentService struct{}

func (h *AgentService) Ask(ctx context.Context, req []byte) ([]byte, error) {
	fmt.Printf("[agent] 被云端调用 Ask: %s\n", string(req))
	return []byte(`{"answer":"库存 12 件","rounds":2}`), nil
}

// RagService 是 ai-rag 注册上去、供 ai-agent 反向调用的服务。
type RagService struct{}

func (h *RagService) Search(ctx context.Context, req []byte) ([]byte, error) {
	fmt.Printf("[rag] 被 agent 调用 Search: %s\n", string(req))
	return []byte(`{"chunks":["退换货 7 天无理由"]}`), nil
}

// ---------------------------------------------------------------- 连接回调

type ServerHandler struct{ slots.Server }

func (h *ServerHandler) OnConnect(ctx context.Context, r *http.Request) error {
	fmt.Printf("[server] 连接建立: %s\n", r.RemoteAddr)
	return nil
}

type ClientHandler struct{ slots.Client }

func (h *ClientHandler) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	fmt.Printf("[client] 连接就绪: userId=%d\n", ch.GetUserId())
	return nil
}
