package nrpc

import (
	"context"
	"time"

	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/metrics"
	"github.com/w6xian/sloth/v4/utils"
)

// 重连退避区间：500ms → 30s 指数退避（带抖动）。
// 抖动是为了避免服务端重启时所有客户端同一时刻集体冲击（惊群）。
const (
	ReconnectMinWait = 500 * time.Millisecond
	ReconnectMaxWait = 30 * time.Second
)

// Backoff 计算第 attempt 次（从 1 开始）失败后的等待时长，带 ±25% 抖动。
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := ReconnectMinWait << uint(attempt-1)
	if d <= 0 || d > ReconnectMaxWait {
		d = ReconnectMaxWait
	}
	jitter := time.Duration(utils.RandInt64(0, int64(d/4)))
	return d - d/8 + jitter
}

// SleepCtx 可被 ctx 取消 / closeCh 关闭打断的等待；返回 false 表示应当退出循环。
//
// closeCh 传 nil 表示"没有这个退出信号"（只读 ctx），此时该分支永久阻塞，
// 语义正确。d <= 0 时不等待，只看退出信号是否已经触发。
func SleepCtx(ctx context.Context, closeCh <-chan struct{}, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-closeCh:
			return false
		default:
			return true
		}
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-closeCh:
		return false
	case <-t.C:
		return true
	}
}

// ReconnectConfig 重连循环的入参：tcp / quic 客户端共用同一套循环，
// 差别只在"怎么建一条连接"（Dial）与日志/指标前缀。
type ReconnectConfig struct {
	// Name 传输名，只用于日志（"tcp" / "quic"）。
	Name string
	// Addr 目标地址，只用于日志。
	Addr string
	// Dial 建一条连接并起它的读写循环，返回该连接结束时会关闭的信号。
	// 返回 nil 表示这次没连上，调用方据此退避重试。
	Dial func(ctx context.Context) (<-chan struct{}, error)
	// Done 调用方已经建好的那条连接的结束信号；可为 nil（循环自己从"没连上"开始）。
	// 传它是为了避免首次拨号成功后循环又立刻建第二条连接。
	Done <-chan struct{}
	// CloseChan 客户端 Close() 时关闭的信号；可为 nil（只看 ctx）。
	CloseChan <-chan struct{}
	// Reconnects 重连尝试计数（失败一次记一次）；可为 nil。
	Reconnects *metrics.Counter
	// Logf 日志输出；可为 nil（不打印）。签名与 logger.Logf 对齐。
	Logf func(level logger.LogLevel, format string, args ...any)
}

// ServeReconnect 后台重连循环：没连接就退避重拨，有连接就等它断，
// 直到 ctx 取消或 CloseChan 关闭。调用方通常 `go ServeReconnect(...)`。
//
// 与 ws 客户端的 KeepAlive 同源：服务端重启、网络抖动对客户端是自动恢复的，
// 不需要业务自己轮询。注意重连只恢复**本地身份**——服务端 bucket 里挂的
// channel 要业务在 OnReady 里重新 Sign/Reg 才会更新。
func ServeReconnect(ctx context.Context, cfg ReconnectConfig) {
	if cfg.Dial == nil {
		return
	}
	done := cfg.Done
	attempt := 0
	for {
		if done == nil {
			attempt++
			if !SleepCtx(ctx, cfg.CloseChan, Backoff(attempt)) {
				return
			}
			d, err := cfg.Dial(ctx)
			if err != nil {
				// 弱网/服务器未起来时失败会连续发生，采样打印避免刷屏
				if cfg.Reconnects != nil {
					cfg.Reconnects.Inc()
				}
				if cfg.Logf != nil && (attempt == 1 || attempt%20 == 0) {
					cfg.Logf(logger.Error, "%s reconnect to %s failed (attempts:%d): %v",
						cfg.Name, cfg.Addr, attempt, err)
				}
				done = nil
				continue
			}
			if cfg.Logf != nil {
				cfg.Logf(logger.Info, "%s reconnected to %s (attempts:%d)", cfg.Name, cfg.Addr, attempt)
			}
			attempt = 0
			done = d
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-cfg.CloseChan:
			return
		case <-done:
			// 连接是被对端/网络断掉的：立刻重连，不再退避等待
			if cfg.Logf != nil {
				cfg.Logf(logger.Info, "%s connection lost, reconnecting to %s", cfg.Name, cfg.Addr)
			}
			done = nil
			attempt = 0
		}
	}
}
