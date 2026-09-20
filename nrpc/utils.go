package nrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/w6xian/sloth/v3/actions"
	"github.com/w6xian/sloth/v3/decoder/fn"
	"github.com/w6xian/sloth/v3/internal/codec"
	"github.com/w6xian/sloth/v3/internal/logger"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types"
	"github.com/w6xian/sloth/v3/types/trpc"
)

func HandleFn(ctx context.Context, r *http.Request, w *http.Response, svr types.IBucket, conn trpc.IConnecter, ch IDataHandler, data []byte) error {
	// use injected codec for decoding so FrameRouter codec can be swapped later
	co, err := codec.GetCodecer(data)
	if err != nil {
		fmt.Println(err.Error())
		return err
	}
	action, id, body, err := co.Decode(data)
	if err != nil {
		return err
	}
	switch action {
	case actions.ACTION_CALL:
		fx := message.GetCallJCO()
		// fx 的 Header/Args 会同步传入 CallFunc，调用返回后即可归还对象池。
		// 原实现取到池对象后从不归还，池退化成"每次新建"，GC 压力翻倍。
		defer message.PutCallJCO(fx)
		err := json.Unmarshal(body, fx)
		if err != nil {
			// 原实现用 log.Println(logger.Error, "…%v", err)：
			// Println 不做格式化，日志级别被当成普通参数打印，真实错误信息丢失。
			log.Printf("%v server readPump，json.Unmarshal err: %v", logger.Error, err)
			return err
		}
		if !conn.IsRegisteredService(fx.Method) {
			resp, lerr := conn.CallNetFunc(ctx, r, fx.Method, id, data)
			ch.Send(ctx, id, resp, lerr)
			return nil
		}
		// 链接通道
		// fx.Channel = ch
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
	case actions.ACTION_REPLY_SUCCESS, actions.ACTION_REPLY_ERROR:
		ch.Receive(ctx, data)
	default:
		log.Printf("server readPump，action:%d is not valid", action)
		return nil
	}
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
