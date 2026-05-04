package wsocket

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/w6xian/sloth/v2/nrpc"
	"github.com/w6xian/sloth/v2/option"
)

// WsListener 是 WsServer 的 nrpc.Listener 适配器
// 由于 WebSocket 基于 HTTP，连接接受由 HTTP 服务器内部处理
// 这个适配器主要用于接口兼容
type WsListener struct {
	server   *WsServer
	addr     string
	mu       sync.Mutex
	closed   bool
	connChan chan nrpc.AuthChannel
}

// NewWsListener 创建 WsServer 的 Listener 适配器
func NewWsListener(server *WsServer, addr string) *WsListener {
	return &WsListener{
		server:   server,
		addr:     addr,
		connChan: make(chan nrpc.AuthChannel, 100),
	}
}

// Accept 实现 nrpc.Listener 接口
// 注意：WebSocket 的连接接受由 HTTP 服务器内部处理
// 这里需要从内部通道获取已接受的连接
func (l *WsListener) Accept() (nrpc.AuthChannel, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, fmt.Errorf("listener closed")
	}
	l.mu.Unlock()

	// 从通道获取已接受的连接
	conn, ok := <-l.connChan
	if !ok {
		return nil, fmt.Errorf("listener closed")
	}
	return conn, nil
}

// Close 实现 nrpc.Listener 接口
func (l *WsListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	close(l.connChan)
	return nil
}

// Addr 实现 nrpc.Listener 接口
func (l *WsListener) Addr() string {
	return l.addr
}

// WsTransportAdapter 是 WebSocket 的 Transport 适配器
// 实现 nrpc.Transport 接口
type WsTransportAdapter struct {
	serverOptions []option.ConnectOption
	clientOptions []ClientOption
	server        *WsServer
	client        *LocalClient
}

// NewWsTransport 创建一个新的 WebSocket Transport 适配器
func NewWsTransport() *WsTransportAdapter {
	return &WsTransportAdapter{}
}

// WithServerOption 设置服务端选项
func (w *WsTransportAdapter) WithServerOption(opts ...option.ConnectOption) *WsTransportAdapter {
	w.serverOptions = append(w.serverOptions, opts...)
	return w
}

// WithClientOption 设置客户端选项
func (w *WsTransportAdapter) WithClientOption(opts ...ClientOption) *WsTransportAdapter {
	w.clientOptions = append(w.clientOptions, opts...)
	return w
}

// Listen 实现 nrpc.Transport 接口
// 启动 WebSocket 服务端，返回 Listener
func (w *WsTransportAdapter) Listen(ctx context.Context, addr string) (nrpc.Listener, error) {
	opts := append(w.serverOptions, option.WithUriPath("/ws"))
	wsServer := NewWsServer(nil, opts...)

	// 解析地址
	_, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	// 启动服务器（在后台运行）
	go func() {
		_ = wsServer.ListenAndServe(ctx)
	}()

	w.server = wsServer
	listener := NewWsListener(wsServer, addr)
	return listener, nil
}

// Dial 实现 nrpc.Transport 接口
// 连接 WebSocket 服务端，返回 ICall
func (w *WsTransportAdapter) Dial(ctx context.Context, addr string) (nrpc.ICall, error) {
	opts := append(w.clientOptions, WithClientServerUri(addr))
	wsClient := NewLocalClient(nil, opts...)

	// 启动客户端连接
	if err := wsClient.ListenAndServe(ctx); err != nil {
		return nil, fmt.Errorf("ws transport dial error: %w", err)
	}

	w.client = wsClient
	return wsClient, nil
}
