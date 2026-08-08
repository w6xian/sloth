package middleware

import (
	"context"

	"github.com/w6xian/sloth/v2/message"
	"github.com/w6xian/sloth/v2/types/auth"
)

// ── 通用类型定义 ──────────────────────────────────────────

// HandlerFunc 服务端 RPC 业务处理函数类型（所有协议通用）。
type HandlerFunc func(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error)

// Middleware 服务端中间件类型。
type Middleware func(HandlerFunc) HandlerFunc

// LogFunc 日志输出函数类型，nil 时使用默认 fmt.Printf 实现。
type LogFunc func(format string, args ...any)

// AuthValidator 自定义鉴权函数类型。
type AuthValidator func(ai *auth.AuthInfo) error
