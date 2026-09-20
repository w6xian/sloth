package sloth

import "github.com/w6xian/sloth/v3/internal/errs"

// 错误判定。
//
// 框架返回的 RPC 错误都可用 errors.Is 判定类型，调用方据此决定行为：
//
//	if errors.Is(err, sloth.ErrTimeout) {
//	    // 可重试（确认方法幂等）
//	} else if errors.Is(err, sloth.ErrNoChannel) {
//	    // 对端离线，重试无意义，直接降级
//	}
//
// 不要去匹配错误文本——文案会变，语义不会。
var (
	// ErrTimeout 等待对端响应超时（可重试，注意幂等）。
	ErrTimeout = errs.ErrTimeout
	// ErrConnClosed 连接已关闭或不可用（需重连后重试）。
	ErrConnClosed = errs.ErrConnClosed
	// ErrQueueFull 待发/回包队列已满（退避/限流，立即重试会加剧拥塞）。
	ErrQueueFull = errs.ErrQueueFull
	// ErrNoChannel 目标用户/房间没有可用连接（对端离线）。
	ErrNoChannel = errs.ErrNoChannel
	// ErrNotServing 本进程尚未建立可用通道（服务端未 Listen / 客户端未 Dial）。
	ErrNotServing = errs.ErrNotServing
	// ErrMethodNotFound 对端未注册该方法（配置/版本问题，重试无意义）。
	ErrMethodNotFound = errs.ErrMethodNotFound
)
