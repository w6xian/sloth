package tcpsock

import (
	"context"

	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/option"
	"github.com/w6xian/sloth/v2/types/trpc"
)

type TcpTransport struct {
	connect trpc.ICallRpc
	opt     *option.Options
}

// NewTcpTransport 创建 TCP Transport 实例。
//
//   - connect：业务层实现的 ICallRpc（处理 RPC 调用）
//   - opt：框架选项（Bucket 大小、Room 大小等）
func NewTcpTransport(connect trpc.ICallRpc, opt *option.Options) *TcpTransport {
	return &TcpTransport{
		connect: connect,
		opt:     opt,
	}
}

// ── nrpc.Transport 接口实现 ─────────────────────────────────────

// Listen 在指定地址启动 TCP 监听，返回 Listener。
func (t *TcpTransport) Listen(ctx context.Context, addr string) (nrpc.Listener, error) {
	srv := NewTcpServer(t.connect, t.opt)
	if _, err := srv.Listen(ctx, addr); err != nil {
		return nil, err
	}
	return srv, nil
}

// Dial 连接远端 TCP 服务端，返回 ICall（可用于 Call/Push）。
func (t *TcpTransport) Dial(ctx context.Context, addr string) (trpc.ICall, error) {
	client := NewTcpClient(t.connect)
	_, err := client.Dial(ctx, addr)
	return client, err
}
