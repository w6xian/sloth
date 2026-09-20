package sloth

import (
	"context"
	"strings"
	"sync"

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

var (
	protocolRegistryMu sync.RWMutex
	protocolRegistry   = map[string]ProtocolFactory{
		"ws":        wsProtocolFactory{},
		"wss":       wsProtocolFactory{secure: true},
		"websocket": wsProtocolFactory{},
		"tcp":       tcpProtocolFactory{},
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
