package sloth

import (
	"context"
	"fmt"
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
//
// p2p 分支：选项里带了 WithCoordinate（且传了 WithWorkID）时，不直接
// net.Listen，而是先经 coordinator 牵线打洞——绑定 UDP socket → 登记
// work-id → 等 client 牵线 → 用 **同一条 socket** 造传输监听器
// （NAT 映射与 socket 绑定，换端口对方进不来，见 p2p 包注释）。
func (s *Connect) makeListenerFor(ctx context.Context, factory ProtocolFactory, address string, opts ...option.ConnectOption) (net.Listener, error) {
	coords, workID, token := resolveP2POpts(opts)
	if len(coords) > 0 {
		coord := coordAddress(coords, string(WS))
		pc, err := punchServeSocket(ctx, address, coord, workID, token)
		if err != nil {
			return nil, err
		}
		if lf, ok := factory.(PacketConnListenerFactory); ok {
			ln, lerr := lf.MakeListenerFromPacketConn(pc, s.tlsConfig)
			if lerr != nil {
				_ = pc.Close()
			}
			return ln, lerr
		}
		_ = pc.Close()
		return nil, fmt.Errorf("protocol %s does not support p2p (no PacketConn listener factory)", factory.Name())
	}
	// 带选项的版本优先：KCP 的加密方式与 FEC 参数决定线上包的格式，
	// 必须在建监听器时就定下来，而此时传输实例还没创建，只能从选项里取。
	if lf, ok := factory.(ListenerFactoryWithOptions); ok {
		return lf.MakeListenerWithOptions(address, s.tlsConfig, opts...)
	}
	if lf, ok := factory.(ListenerFactory); ok {
		return lf.MakeListener(address, s.tlsConfig)
	}
	return net.Listen("tcp", address)
}
