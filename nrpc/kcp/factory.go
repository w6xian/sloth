package kcp

import (
	"context"
	"net"

	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// GetKcpServer 创建 KCP 服务端实例（与 tcp.GetTcpServer 对称）。
//
// 只创建实例：真正的 accept 循环在拿到 listener 之后（Serve 方法），
// 由上层 Connect.Serve 驱动。
func GetKcpServer(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) types.IServer {
	srv := NewKcpServer(c, opts...)
	_ = srv.ListenAndServe(ctx)
	return srv
}

// GetKcpClient 创建 KCP 客户端实例（连接由 ListenAndServe 建立）。
func GetKcpClient(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) trpc.ICall {
	return NewKcpClient(c, opts...)
}

// 编译期断言：服务端必须满足上层要求的接口集合。
var (
	_ types.IServer         = (*KcpServer)(nil)
	_ types.IBucket         = (*KcpServer)(nil)
	_ option.IConnectOption = (*KcpServer)(nil)
	// 流式传输的 accept 循环：上层 Serve 通过它驱动（HTTP 型传输走 Handler()）
	_ interface{ Serve(net.Listener) error } = (*KcpServer)(nil)
)
