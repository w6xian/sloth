package handler

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/types"
)

type IServerHandleMessage interface {
	OnConnect(ctx context.Context, r *http.Request) error
	OnReady(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error
	OnClose(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel) error
	OnData(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, msgType int, message []byte) error
	OnError(ctx context.Context, r *http.Request, s types.IBucket, ch bucket.IChannel, err error) error
}

type IClientHandleMessage interface {
	OnConnect(ctx context.Context, resp *http.Response) error
	OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error
	OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, message []byte) error
	OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error
	OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error
}

// TcpHandleMessage 非 HTTP 传输（TCP）的连接事件钩子。
//
// 注意这里没有 *http.Request / *http.Response：IServerHandleMessage 的每个方法
// 都带着 HTTP 对象，那是 WebSocket 的实现细节漏进了抽象里——TCP 没有 HTTP 握手
// 可传，只能要么伪造一个 *http.Request，要么再定义一套接口。
// 这是"第二实现"逼出来的：钩子接口不能绑死某一层协议。
//
// 放在 types/handler 而不是各传输包内，是因为 option 包要用它构造 ConnectOption
// （option 不能被传输包反向 import，会成环）。
type TcpHandleMessage interface {
	OnConnect(ctx context.Context, addr string) error
	OnReady(ctx context.Context, s types.IBucket, ch bucket.IChannel) error
	OnData(ctx context.Context, s types.IBucket, ch bucket.IChannel, msg []byte) error
	OnClose(ctx context.Context, s types.IBucket, ch bucket.IChannel) error
	OnError(ctx context.Context, s types.IBucket, ch bucket.IChannel, err error) error
}

// TcpClientHandleMessage 非 HTTP 传输（TCP / QUIC）的**客户端**连接事件钩子，
// 与 TcpHandleMessage 对称：没有 *http.Response，客户端也没有 bucket 体系，
// 所以参数里是 bucket.IChannel 而不是 types.IBucket。
//
// 之前这套接口定义在 nrpc/tcp 包内，QUIC 接入时只能再抄一份——两份一模一样的
// 接口意味着 option 层无法用同一个入口注入，因此提到这里与服务端钩子并列。
type TcpClientHandleMessage interface {
	OnConnect(ctx context.Context, addr string) error
	OnReady(ctx context.Context, ch bucket.IChannel) error
	OnData(ctx context.Context, ch bucket.IChannel, msg []byte) error
	OnClose(ctx context.Context, ch bucket.IChannel) error
	OnError(ctx context.Context, ch bucket.IChannel, err error) error
}

type IHandleMessage interface {
	OnConnect(ctx context.Context, r *http.Request, w *http.Response) error
	OnReady(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo) error
	OnData(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo, msgType int, message []byte) error
	OnClose(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo) error
	OnError(ctx context.Context, r *http.Request, w *http.Response, c types.IActionHandler, ch types.IConnInfo, err error) error
}
