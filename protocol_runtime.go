package sloth

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/w6xian/sloth/v3/nrpc/quic"
	"github.com/w6xian/sloth/v3/nrpc/tcp"
	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

// ProtocolFactory 抽象协议实现。所有协议都只需要实现服务器/客户端创建逻辑。
//
// address 是调用方给的原始地址（如 "127.0.0.1:8080"），URL 形态由各传输自己解释：
// ws 要补 ws:// scheme，tcp 用裸 host:port。
//
// 原签名没有 address，只能由上层拼好 URL 再塞进 option——协议细节被推回调用方，
// 工厂退化成一个"是否支持该协议"的标记（上层拿到 factory 后还要再判断
// Name()=="ws" 才敢干活，CreateServer/CreateClient 从未被调用）。
// 这是只有一种实现时看不出来的问题，写第二个实现时才暴露。
type ProtocolFactory interface {
	Name() string
	CreateServer(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (types.IServer, error)
	CreateClient(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (trpc.ICall, error)
}

// ListenerFactory 传输自己创建底层监听器（可选能力）。
//
// 上层默认 net.Listen("tcp", address)，这对 ws/wss/tcp 都成立——它们跑在 TCP 上。
// QUIC 跑在 UDP 上，而且一次 Accept 拿到的是"连接"、真正承载 RPC 的是"流"，
// 所以它必须自己造监听器。这是**第三个实现**逼出来的扩展点：
// 前两种传输下，"监听器 = TCP listener"这个隐含假设从没被质疑过。
// 不实现该接口的传输继续走默认路径。
type ListenerFactory interface {
	MakeListener(address string, tlsConf *tls.Config) (net.Listener, error)
}

type wsProtocolFactory struct{ secure bool }

func (f wsProtocolFactory) Name() string {
	if f.secure {
		return "wss"
	}
	return "ws"
}

func (f wsProtocolFactory) CreateServer(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (types.IServer, error) {
	return wsocket.GetWsServer(ctx, c, opts...), nil
}

func (f wsProtocolFactory) CreateClient(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (trpc.ICall, error) {
	// ws 的地址是 URL：scheme 由本传输自己补，调用方只给的 host:port
	addr := address
	if !strings.Contains(addr, "://") {
		scheme := "ws://"
		if f.secure {
			scheme = "wss://"
		}
		addr = scheme + addr
	}
	opts = append([]option.ConnectOption{
		option.WithUriPath("/ws"),
		option.WithAddress(addr),
	}, opts...)
	return wsocket.GetWsClient(ctx, c, opts...), nil
}

type tcpProtocolFactory struct{}

func (tcpProtocolFactory) Name() string { return "tcp" }

func (tcpProtocolFactory) CreateServer(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (types.IServer, error) {
	return tcp.GetTcpServer(ctx, c, opts...), nil
}

func (tcpProtocolFactory) CreateClient(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (trpc.ICall, error) {
	opts = append([]option.ConnectOption{option.WithAddress(address)}, opts...)
	return tcp.GetTcpClient(ctx, c, opts...), nil
}

// quicProtocolFactory QUIC（UDP）传输。
//
// 与另两个工厂的差异只有一处：它额外实现了 ListenerFactory，
// 因为底层不是 TCP listener（见 ListenerFactory 注释）。
type quicProtocolFactory struct{}

func (quicProtocolFactory) Name() string { return "quic" }

// MakeListener QUIC 跑在 UDP 上，监听器由传输自己造。
// tlsConf 为 nil 时 quic.Listen 会直接报错——QUIC 没有 TLS 就无法握手。
func (quicProtocolFactory) MakeListener(address string, tlsConf *tls.Config) (net.Listener, error) {
	return quic.MakeListener(address, tlsConf)
}

func (quicProtocolFactory) CreateServer(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (types.IServer, error) {
	return quic.GetQuicServer(ctx, c, opts...), nil
}

func (quicProtocolFactory) CreateClient(ctx context.Context, c trpc.ICallRpc, address string, opts ...option.ConnectOption) (trpc.ICall, error) {
	// QUIC 的 TLS 是必填项：客户端同样要有一份 tls.Config，
	// 证书校验策略（是否跳过自签证书）由业务自己定。
	var tlsConf *tls.Config
	if cc, ok := c.(interface{ TLSConfig() *tls.Config }); ok {
		tlsConf = cc.TLSConfig()
	}
	if tlsConf == nil {
		return nil, errors.New("quic requires TLS: set sloth.WithTLSConfig(...) before Dial")
	}
	cli := quic.NewQuicClient(c, append([]option.ConnectOption{option.WithAddress(address)}, opts...)...)
	cli.SetTLSConfig(tlsConf)
	return cli, nil
}

var (
	protocolRegistryMu sync.RWMutex
	protocolRegistry   = map[string]ProtocolFactory{
		"ws":        wsProtocolFactory{},
		"wss":       wsProtocolFactory{secure: true},
		"websocket": wsProtocolFactory{},
		"tcp":       tcpProtocolFactory{},
		"quic":      quicProtocolFactory{},
	}
)

func RegisterProtocol(name string, factory ProtocolFactory) {
	protocolRegistryMu.Lock()
	defer protocolRegistryMu.Unlock()
	protocolRegistry[name] = factory
}

func ResolveProtocol(name string) ProtocolFactory {
	protocolRegistryMu.RLock()
	defer protocolRegistryMu.RUnlock()
	factory, ok := protocolRegistry[name]
	if !ok {
		return nil
	}
	return factory
}

func GetProtocolFactory(name string) ProtocolFactory {
	return ResolveProtocol(name)
}

// snapshotProtocols 拷贝一份当前注册表。
//
// Connect 实例持有一份快照，避免实例的注册影响全局；同时 resolveProtocol 会在
// 快照未命中时回查全局表，因此后注册的协议也能生效。
// 此前两套注册机制（实例表 / 全局表）完全不互通，全局 RegisterProtocol 注册的实现
// 永远不会被用到——等于死代码。
func snapshotProtocols() map[string]ProtocolFactory {
	protocolRegistryMu.RLock()
	defer protocolRegistryMu.RUnlock()
	m := make(map[string]ProtocolFactory, len(protocolRegistry))
	for k, v := range protocolRegistry {
		m[k] = v
	}
	return m
}
