package sloth

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
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
