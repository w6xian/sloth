package sloth

import (
	"context"
	"net"

	"github.com/w6xian/sloth/v4/option"
	"github.com/w6xian/sloth/v4/types"
)

// ProtocolListener 协议监听器
type ProtocolListener struct {
	Network   string                 // 协议类型: ws, tcp, quic, grpc
	Address   string                 // 监听地址
	Context   context.Context        // 监听上下文
	Listener net.Listener  // net.Listener 监听器
	Server   types.IServer // 由 ProtocolFactory 创建的传输实例
	Options   []option.ConnectOption // 连接	 选项
}

// makeListenerFor 按传输类型造底层监听器。
//
// 默认走明文 TCP（ws / wss / tcp 都跑在 TCP 上）；QUIC 实现了 ListenerFactory，
// 由它自己造 UDP 监听器——硬编码 net.Listen("tcp") 会让 QUIC 的监听地址
// 变成一个根本不收 QUIC 包的 TCP 端口（握手永不成功，且没有任何报错）。
//
// 是否套 TLS 一律由传输自己决定，**不看 Connect.tlsConfig 是否为 nil**：
// QUIC 强制要求 tlsConfig，照它判断会把同 Connect 上的 ws / tcp 端口一起
// 变成 TLS 监听器，明文客户端全部握手失败且不报错。TLS 该在哪一层生效：
// wss → http.Server.ServeTLS（握手阶段），QUIC → quic-go 内部，
// 两者都不需要 TLS listener。
func (s *Connect) makeListenerFor(factory ProtocolFactory, address string) (net.Listener, error) {
	if lf, ok := factory.(ListenerFactory); ok {
		return lf.MakeListener(address, s.tlsConfig)
	}
	return net.Listen("tcp", address)
}
