package quic

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/sloth/v4/types/trpc"
)

// GetQuicServer 创建 QUIC 服务端实例（与 tcp.GetTcpServer 对称）。
//
// 只创建实例：真正的 accept 循环在拿到 listener 之后（Serve 方法），
// 由上层 Connect.Serve 驱动。
func GetQuicServer(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) types.IServer {
	srv := NewQuicServer(c, opts...)
	_ = srv.ListenAndServe(ctx)
	return srv
}

// GetQuicClient 创建 QUIC 客户端实例（连接由 ListenAndServe 建立）。
func GetQuicClient(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) trpc.ICall {
	return NewQuicClient(c, opts...)
}

// MakeListener 创建 QUIC 的底层监听器。
//
// 抽象第四课：上层原本一律 net.Listen("tcp", address)——ws/wss/tcp 都跑在 TCP 上，
// 这个假设一直成立。QUIC 跑在 UDP 上，而且 Accept 出来的是"流"不是"连接"，
// 所以监听器必须由传输自己造（见 Listener 的注释），
// 上层为此多了一个可选接口 ListenerFactory。
func MakeListener(address string, tlsConf *tls.Config) (net.Listener, error) {
	return Listen(address, tlsConf, defaultQuicConfig())
}

// 编译期断言：服务端必须满足上层要求的接口集合。
var (
	_ types.IServer         = (*QuicServer)(nil)
	_ types.IBucket         = (*QuicServer)(nil)
	_ option.IConnectOption = (*QuicServer)(nil)
	// 流式传输的 accept 循环：上层 Serve 通过它驱动（HTTP 型传输走 Handler()）
	_ interface{ Serve(net.Listener) error } = (*QuicServer)(nil)
	// QUIC stream 补成 net.Conn 后即可复用公共字节流通道
	_ net.Conn = (*streamConn)(nil)
)
