package main

// relay 的 sloth 接入层：ws 监听、Register 服务、连接生命周期钩子。

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/tools/tunnel/proto"
	"github.com/w6xian/sloth/v4/types"
	"github.com/w6xian/tlv"
)

// RelayService agent 拨入后调用的 RPC 服务（方法名 tunnel.Register）。
type RelayService struct {
	r *Relay
}

// registerReq agent 携带的登记信息。
type registerReq struct {
	Name    string        `json:"name"`
	Tunnels []agentTunnel `json:"tunnels"`
}

type agentTunnel struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// registerResp relay 回给 agent 的接管结果。
type registerResp struct {
	Accepted []string `json:"accepted"`
}

// Register agent 上线登记：校验每个 (id, token)，通过的隧道绑定到这条连接。
//
// agent 断线重连后会再次调用（框架 ws 客户端自带重连，OnReady 每次连接成功
// 都会触发登记）；重连登记会替换旧连接，旧连接的在途访客由 OnClose 收口。
func (s *RelayService) Register(ctx context.Context, req []byte) ([]byte, error) {
	ch, err := sloth.GetChannel(ctx)
	if err != nil {
		return nil, err
	}
	var rr registerReq
	if err := tlv.Json2Struct(req, &rr); err != nil {
		return nil, fmt.Errorf("bad register payload: %w", err)
	}
	if len(rr.Tunnels) == 0 {
		return nil, fmt.Errorf("no tunnels in register payload")
	}

	resp := registerResp{}
	for _, at := range rr.Tunnels {
		t, ok := s.r.tunnels[at.ID]
		// token 用常数时间比较，避免计时侧信道
		if !ok || subtle.ConstantTimeCompare([]byte(at.Token), []byte(t.cfg.Token)) != 1 {
			sloth.Errorw(ctx, "tunnel register denied", "tunnel", at.ID, "agent", rr.Name)
			continue
		}
		t.setChannel(ch)
		resp.Accepted = append(resp.Accepted, at.ID)
		sloth.Infow(ctx, "tunnel online", "tunnel", at.ID, "agent", rr.Name)
	}
	return tlv.Json(resp), nil
}

// relayHandler relay 的 ws 连接生命周期钩子：只关心 agent 推回的数据帧与断连。
type relayHandler struct {
	r *Relay
}

func (h *relayHandler) OnConnect(ctx context.Context, req *http.Request) error {
	return nil
}

func (h *relayHandler) OnReady(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel) error {
	return nil
}

func (h *relayHandler) OnData(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel, msgType int, msg []byte) error {
	// agent 用 client.Send 推帧，wire 上是 message.Msg 的 JSON 信封，
	// 先解封还原隧道帧；注册前（无隧道归属）的帧会在 handleAgentFrame
	// 里因查不到连接被自然丢弃。
	frame, err := proto.UnwrapEnvelope(msg)
	if err != nil {
		sloth.Errorw(ctx, "bad push from agent", "err", err)
		return nil
	}
	h.r.handleAgentFrame(ctx, frame)
	return nil
}

func (h *relayHandler) OnClose(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel) error {
	h.r.onAgentGone(ctx, ch)
	return nil
}

func (h *relayHandler) OnError(ctx context.Context, req *http.Request, s types.IBucket, ch bucket.IChannel, err error) error {
	if ch != nil {
		h.r.onAgentGone(ctx, ch)
	}
	return nil
}
