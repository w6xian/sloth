package kcpsock

import (
	"context"

	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/types/trpc"
)

// KcpTransport 实现 nrpc.Transport 接口，提供 KCP 协议的
// Listen（服务端监听）和 Dial（客户端连接）能力。
//
// 用法：
//
//	transport := NewKcpTransport(connect, opt)
//
//	// 服务端
//	listener, _ := transport.Listen(ctx, ":8080")
//	for {
//	    ch, _ := listener.Accept()
//	    go handleChannel(ch)
//	}
//
//	// 客户端
//	client, _ := transport.Dial(ctx, "127.0.0.1:8080")
//	rsp, err := client.Call(ctx, header, "MethodName", arg)
type KcpTransport struct {
	connect trpc.ICallRpc
	opt     *option.Options
}

// NewKcpTransport 创建 KCP Transport 实例。
//
//   - connect：业务层实现的 ICallRpc（处理 RPC 调用）
//   - opt：框架选项（Bucket 大小、Room 大小等）
func NewKcpTransport(connect trpc.ICallRpc, opt *option.Options) *KcpTransport {
	return &KcpTransport{
		connect: connect,
		opt:     opt,
	}
}

// ── nrpc.Transport 接口实现 ─────────────────────────────────────

// Listen 在指定地址启动 KCP 监听，返回 Listener。
func (t *KcpTransport) Listen(ctx context.Context, addr string) (nrpc.Listener, error) {
	srv := NewKcpServer(t.connect, t.opt)
	if _, err := srv.Listen(ctx, addr); err != nil {
		return nil, err
	}
	return srv, nil
}

// Dial 连接远端 KCP 服务端，返回 ICall（可用于 Call/Push）。
func (t *KcpTransport) Dial(ctx context.Context, addr string) (trpc.ICall, error) {
	client := NewKcpClient(t.connect)
	_, err := client.Dial(ctx, addr)
	return client, err
}
