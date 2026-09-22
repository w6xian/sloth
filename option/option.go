package option

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/types/handler"
)

type IConnectOption interface {
	SetUriPath(path string) error
	SetRouter(router *mux.Router) error
	SetAddress(address string) error
	SetServerHandleMessage(handler handler.IServerHandleMessage) error
	SetClientHandleMessage(handler handler.IClientHandleMessage) error
	SetHeader(key string, value string) error
	SetOrigin(origins ...string) error
	SetCodec(c codec.Codec)
}

type ConnectOption func(s IConnectOption)

func (o *ConnectOption) String() string {
	return "ConnectOption"
}

func WithUriPath(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetUriPath(path)
	}
}

func WithAddress(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetAddress(path)
	}
}

func WithRouter(router *mux.Router, path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
		http.Handle(path, router)
	}
}
func WithRouterWithoutHandle(router *mux.Router) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
	}
}

func WithServerHandleMessage(handler handler.IServerHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetServerHandleMessage(handler)
	}

}

func WithClientHandleMessage(handler handler.IClientHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetClientHandleMessage(handler)
	}
}

func WithRequestHeader(key string, value string) ConnectOption {
	return func(s IConnectOption) {
		s.SetHeader(key, value)
	}
}

func WithOrigin(origins ...string) ConnectOption {
	return func(s IConnectOption) {
		s.SetOrigin(origins...)
	}
}

func WithCodec(c codec.Codec) ConnectOption {
	return func(s IConnectOption) {
		s.SetCodec(c)
	}
}

// WithTcpHandleMessage 给非 HTTP 传输（TCP）注入连接事件钩子。
//
// 为什么是独立的一个 option：IConnectOption.SetServerHandleMessage 的形参写死了
// handler.IServerHandleMessage（每个方法都带 *http.Request），TCP 没有 HTTP 握手，
// 传它进去会被静默忽略。这个 option 走"可选接口"：只有实现了
// SetTcpHandleMessage 的传输才会收到（与 WithChannelQueueSize 同一套路），
// WebSocket 传输传了也不会有效果——它本来就有 HTTP 版钩子。
func WithTcpHandleMessage(h handler.TcpHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		if v, ok := any(s).(interface{ SetTcpHandleMessage(handler.TcpHandleMessage) }); ok {
			v.SetTcpHandleMessage(h)
		}
	}
}

// WithTcpClientHandleMessage 给非 HTTP 传输（TCP / QUIC）的**客户端**注入
// 连接事件钩子，是 WithTcpHandleMessage（服务端）的对称入口。
//
// 同样走"可选接口"：只有实现了 SetTcpClientHandleMessage 的客户端才会收到。
// HTTP 版客户端钩子（WithClientHandleMessage）对它们无效——那些方法带
// *http.Response，非 HTTP 传输没有握手响应可传。
func WithTcpClientHandleMessage(h handler.TcpClientHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		if v, ok := any(s).(interface {
			SetTcpClientHandleMessage(handler.TcpClientHandleMessage)
		}); ok {
			v.SetTcpClientHandleMessage(h)
		}
	}
}

// WithChannelQueueSize 设置每条连接的队列容量（待发 RPC / 回包 / 广播）。
//
// 容量是背压的第一道闸门：太小→突发流量下频繁"队列满"；
// 太大→内存占用高、延迟被缓冲掩盖。默认 10。
// 仅对实现 SetChannelQueueSize 的传输层生效（ws 服务端与客户端均支持）。
func WithChannelQueueSize(n int) ConnectOption {
	return func(s IConnectOption) {
		if v, ok := any(s).(interface{ SetChannelQueueSize(int) }); ok {
			v.SetChannelQueueSize(n)
		}
	}
}

// WithDebugHandler 把调试端点（/debug/metrics、/debug/pprof/*、/debug/vars）
// 挂到当前服务端的 HTTP 路由上，与业务端口共用监听地址。
//
// 仅建议内网环境：端点无鉴权。对外服务请用 option.WithDebugAddr 起独立端口，
// 或自行用 http.ServeMux 挂载（DebugHandler 返回的 handler）。
//
// 该选项只对实现 SetDebug 的服务端（如 *wsocket.WsServer）生效，客户端侧忽略。
func WithDebugHandler() ConnectOption {
	return func(s IConnectOption) {
		if v, ok := any(s).(interface{ SetDebug(bool) }); ok {
			v.SetDebug(true)
		}
	}
}
