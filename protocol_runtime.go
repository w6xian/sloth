package sloth

import (
	"context"
	"sync"

	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

// ProtocolFactory 抽象协议实现。所有协议都只需要实现服务器/客户端创建逻辑，
// 不再侵入原有 ws 逻辑，后续添加 tcp/quic/kcp 时只扩展这个工厂即可。
type ProtocolFactory interface {
	Name() string
	CreateServer(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) (types.IServer, error)
	CreateClient(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) (trpc.ICall, error)
}

type wsProtocolFactory struct{}

func (wsProtocolFactory) Name() string { return "ws" }

func (wsProtocolFactory) CreateServer(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) (types.IServer, error) {
	return wsocket.GetWsServer(ctx, c, opts...), nil
}

func (wsProtocolFactory) CreateClient(ctx context.Context, c trpc.ICallRpc, opts ...option.ConnectOption) (trpc.ICall, error) {
	return wsocket.GetWsClient(ctx, c, opts...), nil
}

var (
	protocolRegistryMu sync.RWMutex
	protocolRegistry   = map[string]ProtocolFactory{
		"ws":        wsProtocolFactory{},
		"wss":       wsProtocolFactory{},
		"websocket": wsProtocolFactory{},
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
