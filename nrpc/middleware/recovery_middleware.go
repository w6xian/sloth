package middleware

import (
	"context"
	"fmt"

	"github.com/w6xian/sloth/v2/message"
)

// Recovery 创建一个 Panic 恢复中间件（服务端）。
func Recovery(logger LogFunc, tag string) Middleware {
	if logger == nil {
		logger = func(format string, args ...any) {
			fmt.Printf("[%s] "+format+"\n", args...)
		}
	}
	if tag == "" {
		tag = "rpc-server"
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {

			defer func() {
				if r := recover(); r != nil {
					logger("%s panic recovered  → method=%s panic=%v",
						tag, mtd, r)
				}
			}()
			return next(ctx, header, mtd, args...)
		}
	}
}
