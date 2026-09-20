package tcp

import (
	"context"
	"net"

	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// GetTcpServer 创建 TCP 服务端实例（与 wsocket.GetWsServer 对称）。
//
// 注意：这里只创建实例，真正的 accept 循环在拿到 net.Listener 之后（Serve 方法），
// 由上层 Connect.Serve 驱动。
func GetTcpServer(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) types.IServer {
	srv := NewTcpServer(c, opts...)
	_ = srv.ListenAndServe(ctx)
	return srv
}

// GetTcpClient 创建 TCP 客户端实例（连接由 ListenAndServe 建立）。
func GetTcpClient(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) trpc.ICall {
	return NewTcpClient(c, opts...)
}

// 编译期断言：服务端必须满足上层要求的接口集合。
var (
	_ types.IServer         = (*TcpServer)(nil)
	_ types.IBucket         = (*TcpServer)(nil)
	_ option.IConnectOption = (*TcpServer)(nil)
	// 流式传输的 accept 循环：上层 Serve 通过它驱动（HTTP 型传输走 Handler()）
	_ interface{ Serve(net.Listener) error } = (*TcpServer)(nil)
)
