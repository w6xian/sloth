package nrpc

import (
	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/types/auth"
)

// 这里原本还有 Transport / Listener 两个接口（"各协议实现此接口"），但它们
// 从未有过实现者：WebSocket 的那份适配器整文件都是注释状态，TCP 则用更贴合
// 现实的形态接入（IServer + 可选的 Handler()/Serve(ln)）。
// 留着只会让人以为"实现这两个接口就能接入"，实际接入点是 ProtocolFactory，
// 因此删除；真正生效的传输契约见 protocol_runtime.go 与 types.IServer。

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
	GetAuthInfo() (*auth.AuthInfo, error)
	SetAuthInfo(auth *auth.AuthInfo) error
}

// ── TLV 帧类型（预留，尚未在链路上使用）────────────────────────
//
// 帧格式（对应 github.com/w6xian/tlv 格式）：
//
//	┌────────┬──────────┬──────────────────┐
//	│  Type  │  Length  │      Value       │
//	│  (1B) │  (4B)    │   (Length B)    │
//	└────────┴──────────┴──────────────────┘
//
// 注意：当前 RPC 链路上跑的是 FN 帧（见 decoder/fn），不是这里的 TLV 帧。
// 这些常量只在 TLV 解析路径里使用（nrpc/wsocket 的 tlvValue），
// 曾被称为"所有协议共用的统一帧格式"，与实现不符，容易误导，故更正说明。
const (
	FrameTypeCall  byte = 0x01 // RPC Call 请求（客户端 → 服务端）
	FrameTypeReply byte = 0x02 // RPC Reply 成功（服务端 → 客户端）
	FrameTypeError byte = 0x03 // RPC Reply 错误（服务端 → 客户端）
	FrameTypePush  byte = 0x04 // Push/Broadcast 消息（单向）
	FrameTypePing  byte = 0x05 // 心跳 Ping
	FrameTypePong  byte = 0x06 // 心跳 Pong
)
