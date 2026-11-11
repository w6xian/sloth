package main

// agent 的 sloth 接入层：连接生命周期钩子。

import (
	"context"
	"net/http"

	"github.com/w6xian/sloth/v4/tools/tunnel/proto"
	"github.com/w6xian/sloth/v4/types"
)

// agentHandler agent 的 ws 连接生命周期钩子。
type agentHandler struct {
	a *Agent
}

func (h *agentHandler) OnConnect(ctx context.Context, resp *http.Response) error {
	return nil
}

// OnReady 每次连接建立（含框架自动重连后的再建立）都会触发：登记隧道。
// 登记是幂等的——relay 侧按 (id, token) 校验并重新绑定到这条新连接。
func (h *agentHandler) OnReady(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	go h.a.tryRegister()
	return nil
}

func (h *agentHandler) OnData(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, msgType int, msg []byte) error {
	// relay 用 ch.Push 推帧，wire 上是 message.Msg 的 JSON 信封，先解封。
	frame, err := proto.UnwrapEnvelope(msg)
	if err != nil {
		return nil // 非隧道帧（如框架自身的信封变化），静默忽略
	}
	h.a.handleFrame(h.a.runCtx, frame)
	return nil
}

func (h *agentHandler) OnClose(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo) error {
	h.a.teardown()
	return nil
}

func (h *agentHandler) OnError(ctx context.Context, resp *http.Response, c types.IConnRpc, ch types.IConnInfo, err error) error {
	// OnError 之后 readPump 会退出并触发 OnClose，这里不重复 teardown，
	// 只把登记状态提前清掉，让重连后的 OnReady 一定重新登记。
	h.a.registered.Store(false)
	return nil
}
