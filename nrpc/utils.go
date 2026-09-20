package nrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/decoder/fn"
	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/internal/metrics"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

// RPC 服务端指标（包级注册，label 固定）。
var (
	rpcCalls        = metrics.NewCounter(`sloth_rpc_calls_total{action="call"}`, "服务端收到的 RPC 调用次数")
	rpcReplies      = metrics.NewCounter(`sloth_rpc_calls_total{action="reply"}`, "服务端收到的 RPC 响应次数")
	rpcInvalid      = metrics.NewCounter(`sloth_rpc_calls_total{action="invalid"}`, "无法识别的 action 次数")
	rpcDecodeErrors = metrics.NewCounter("sloth_rpc_decode_errors_total", "RPC 报文解码失败次数")
	rpcCallDuration = metrics.NewHistogram("sloth_rpc_call_duration_seconds", "服务端处理单次 RPC 调用耗时（秒）", nil)
)

func HandleFn(ctx context.Context, r *http.Request, w *http.Response, svr types.IBucket, conn trpc.IConnecter, ch IDataHandler, data []byte) error {
	// use injected codec for decoding so FrameRouter codec can be swapped later
	co, err := codec.GetCodecer(data)
	if err != nil {
		// 取不到编解码器通常是报文格式不认识，属于调用方问题，用 Warn 而非 Error
		logger.Warnw(ctx, "rpc codec lookup failed", "err", err)
		return err
	}
	action, id, body, err := co.Decode(data)
	if err != nil {
		rpcDecodeErrors.Inc()
		return err
	}
	switch action {
	case actions.ACTION_CALL:
		return handleCall(ctx, r, w, svr, conn, ch, data, id, body)
	case actions.ACTION_REPLY_SUCCESS, actions.ACTION_REPLY_ERROR:
		rpcReplies.Inc()
		ch.Receive(ctx, data)
	default:
		rpcInvalid.Inc()
		logger.Warnw(ctx, "rpc unknown action", "action", action)
		return nil
	}
	return nil
}

// handleCall 处理一次 RPC 调用请求。
func handleCall(ctx context.Context, r *http.Request, w *http.Response, svr types.IBucket,
	conn trpc.IConnecter, ch IDataHandler, raw []byte, id uint64, body []byte) error {
	start := time.Now()
	defer func() {
		rpcCalls.Inc()
		rpcCallDuration.Observe(time.Since(start))
	}()

	fx := message.GetCallJCO()
	// fx 的 Header/Args 会同步传入 CallFunc，调用返回后即可归还对象池。
	// 原实现取到池对象后从不归还，池退化成"每次新建"，GC 压力翻倍。
	defer message.PutCallJCO(fx)
	if err := json.Unmarshal(body, fx); err != nil {
		// 原实现用 log.Println(logger.Error, "…%v", err)：
		// Println 不做格式化，级别被当成普通参数打印，真实错误信息丢失。
		rpcDecodeErrors.Inc()
		logger.Errorw(ctx, "rpc json.Unmarshal failed", "err", err, "msgId", id)
		return err
	}
	// 客户端通过 Header 透传 trace id（键：X-Trace-Id），缺失时本地生成，
	// 使两端日志能按同一次调用串联。
	// 注意：JsonCallObject.Header 声明为 map[string]string（无方法），需转为 message.Header 才能用 Get。
	// 这里不无条件 WithFields 挂 method/msgId：正常路径用不上（每次调用 2 次额外分配），
	// 出错时才把它作为日志字段带上。
	if trace := message.Header(fx.Header).Get(logger.TraceHeader); trace != "" {
		ctx = logger.WithTraceID(ctx, trace)
	} else {
		ctx, _ = logger.EnsureTrace(ctx)
	}

	if !conn.IsRegisteredService(fx.Method) {
		resp, lerr := conn.CallNetFunc(ctx, r, fx.Method, id, raw)
		ch.Send(ctx, id, resp, lerr)
		return nil
	}
	// 调用 connect.CallFunc 方法
	rst, err := conn.CallFunc(ctx, r, w, svr, &trpc.RpcCaller{
		Method:  fx.Method,
		Data:    body,
		Channel: ch,
		Header:  fx.Header,
		Args:    fx.Args,
	})
	ch.Send(ctx, id, rst, err)
	return nil
}

// defaultTimeout 超时配置为 0（未设置）时的兜底值，避免 timer 立即触发把正常调用判成超时。
const defaultTimeout = 10 * time.Second

// 服务器调用客户端方法
func CallFuncWithResult(ctx context.Context, msgId uint64, payload []byte, sender DataChannel, timeout TimeOut) ([]byte, error) {

	writeTimeout := timeout.Write
	if writeTimeout <= 0 {
		writeTimeout = defaultTimeout
	}
	// 用 Timer 而非 Ticker：
	// Ticker 的通道会缓存一次到期信号，Reset 并不会排空它，
	// 写入阶段遗留的到期信号会让下一阶段立刻"假超时"（并发 RPC 下偶发 call timeout）。
	timer := time.NewTimer(writeTimeout)
	defer timer.Stop()
	// 发送调用请求
	select {
	case <-timer.C:
		return []byte{}, fmt.Errorf("call timeout")
	case sender.Write <- payload:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// 复用 timer 前必须排空已到期的信号（Stop 返回 false 表示信号已发出/已被取走）
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	replyTimeout := timeout.Read
	if replyTimeout <= 0 {
		replyTimeout = writeTimeout
	}
	timer.Reset(replyTimeout)
	// 等待调用结果
	for {
		select {
		case <-ctx.Done():
			return []byte{}, ctx.Err()
		case <-timer.C:
			return []byte{}, fmt.Errorf("reply timeout")
		case raw, ok := <-sender.Read:
			if !ok {
				return []byte{}, fmt.Errorf("rpc result closed")
			}
			action, aerr := fn.Action(raw)
			if aerr != nil {
				return []byte{}, aerr
			}
			switch action {
			case actions.ACTION_REPLY_SUCCESS:
				if fn.Id(raw) != msgId {
					continue
				}
				return fn.Data(raw), nil
			case actions.ACTION_REPLY_ERROR:
				if fn.Id(raw) != msgId {
					continue
				}
				return []byte{}, errors.New(string(fn.Data(raw)))
			default:
				return []byte{}, fmt.Errorf("action not match")
			}
		}
	}
}
