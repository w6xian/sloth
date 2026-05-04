package nrpc

import (
	"context"

	"github.com/w6xian/sloth/v2/bucket"
)

// Transport 各协议实现此接口，统一服务端 Listen 和客户端 Dial。
// 每个协议（TCP、QUIC、WebSocket、KCP、UDP）分别实现一个 Transport。
type Transport interface {
	// Listen 启动服务端监听，返回 Listener。
	Listen(ctx context.Context, addr string) (Listener, error)
	// Dial 连接远端服务端，返回 ICall（用于 Call/Push）。
	Dial(ctx context.Context, addr string) (ICall, error)
}

// Listener 服务端监听器接口。
// Accept 返回 AuthChannel，可接入 bucket 体系并支持 Auth。
type Listener interface {
	Accept() (AuthChannel, error)
	Close() error
	Addr() string
}

// AuthChannel 服务端 Channel 接口，在 bucket.IChannel 基础上增加 Auth 方法。
// 各协议的服务端 Channel 实现此接口即可接入 bucket 体系。
//
// bucket.IChannel 提供：
//
//	Call(ctx, header, mtd, args...) ([]byte, error)
//	Push(ctx, msg) error
//	ReplySuccess(id, data) error
//	ReplyError(id, err) error
//	Prev/Next/Room/UserId/Token/Close  （bucket 链表管理）
//
// AuthChannel 额外提供：
//
//	GetAuthInfo() / SetAuthInfo()  （身份认证）
type AuthChannel interface {
	bucket.IChannel
	GetAuthInfo() (*AuthInfo, error)
	SetAuthInfo(auth *AuthInfo) error
}

// ── 统一 TLV 帧类型（所有协议共用）─────────────────────────────
//
// 帧格式（对应 github.com/w6xian/tlv 格式）：
//
//	┌────────┬──────────┬──────────────────┐
//	│  Type  │  Length  │      Value       │
//	│  (1B) │  (4B)    │   (Length B)    │
//	└────────┴──────────┴──────────────────┘
const (
	FrameTypeCall  byte = 0x01 // RPC Call 请求（客户端 → 服务端）
	FrameTypeReply byte = 0x02 // RPC Reply 成功（服务端 → 客户端）
	FrameTypeError byte = 0x03 // RPC Reply 错误（服务端 → 客户端）
	FrameTypePush  byte = 0x04 // Push/Broadcast 消息（单向）
	FrameTypePing  byte = 0x05 // 心跳 Ping
	FrameTypePong  byte = 0x06 // 心跳 Pong
)
