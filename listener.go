package sloth

import (
	"context"
	"crypto/tls"
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

// block can be nil if the caller wishes to skip encryption in kcp.
// tlsConfig can be nil iff we are not using network "quic".
func (s *Connect) makeListener(network, address string) (ln net.Listener, err error) {
	if s.tlsConfig == nil {
		ln, err = net.Listen(network, address)
	} else {
		ln, err = tls.Listen(network, address, s.tlsConfig)
	}
	return ln, err
}

// makeListenerFor 按传输类型造底层监听器。
//
// 默认走 TCP（ws / wss / tcp 都跑在 TCP 上）；QUIC 实现了 ListenerFactory，
// 由它自己造 UDP 监听器——硬编码 net.Listen("tcp") 会让 QUIC 的监听地址
// 变成一个根本不收 QUIC 包的 TCP 端口（握手永不成功，且没有任何报错）。
func (s *Connect) makeListenerFor(factory ProtocolFactory, address string) (net.Listener, error) {
	if lf, ok := factory.(ListenerFactory); ok {
		return lf.MakeListener(address, s.tlsConfig)
	}
	return s.makeListener("tcp", address)
}
