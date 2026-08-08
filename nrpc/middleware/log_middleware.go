package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/w6xian/sloth/v2/message"
)

// ClientLog 创建一个客户端日志中间件，记录每次 RPC 调用的耗时和错误。
func ClientLog(logger LogFunc, tag string) Middleware {
	if logger == nil {
		logger = func(format string, args ...any) {
			fmt.Printf("[%s] "+format+"\n", args...)
		}
	}
	if tag == "" {
		tag = "rpc-client"
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
			start := time.Now()
			logger("%s call start  → method=%s", tag, mtd)
			data, err := next(ctx, header, mtd, args...)
			duration := time.Since(start)
			if err != nil {
				logger("%s call error  → method=%s duration=%s err=%v", tag, mtd, duration, err)
			} else {
				logger("%s call done  → method=%s duration=%s", tag, mtd, duration)
			}
			return data, err
		}
	}
}
