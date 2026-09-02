package sloth

import (
	"context"

	"github.com/w6xian/sloth/v3/nrpc/wsocket"
	"github.com/w6xian/sloth/v3/option"
)

// path是uri中的路径，默认是"/ws"
func (c *Connect) initWsServerInstance(ctx context.Context, opts ...option.ConnectOption) error {
	// create concrete WsServer so we can inject protocol-specific codec
	wsServer := wsocket.NewWsServer(c, opts...)
	// 保存实例：Serve() 用它挂载 HTTP handler（此前 http.Serve(listener, nil)
	// 与 WsServer 的 mux 路由脱节），Close() 用它优雅关闭活跃连接
	c.wsServer = wsServer
	c.client.setServe(wsServer)
	// 仅注册 /ws 路由与 OnConnect 校验，真正的 HTTP server 由 Serve() 启动
	return wsServer.ListenAndServe(ctx)
}

func (c *Connect) initWsClientInstance(ctx context.Context, opts ...option.ConnectOption) error {
	//set the maximum number of CPUs that can be executing
	wsClient := wsocket.NewLocalClient(c, opts...)
	// ServerRpc.Listen 可能被另一 goroutine 的 Call/SetAuthInfo 读取，需加锁写入
	c.server.setListen(wsClient)
	return wsClient.ListenAndServe(ctx)
}
