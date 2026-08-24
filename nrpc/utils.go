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
		err := json.Unmarshal(body, fx)
		if err != nil {
			log.Println(logger.Error, "server readPump，json.Unmarshal err:%v", err)
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

// 服务器调用客户端方法
func CallFuncWithResult(ctx context.Context, msgId uint64, payload []byte, sender DataChannel, timeout TimeOut) ([]byte, error) {

	ticker := time.NewTicker(timeout.Write)
	defer ticker.Stop()
	// 发送调用请求
	select {
	case <-ticker.C:
		return []byte{}, fmt.Errorf("call timeout")
	case sender.Write <- payload:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ticker.Reset(timeout.Write)
	// 等待调用结果
	for {
		select {
		case <-ctx.Done():
			return []byte{}, ctx.Err()
		case <-ticker.C:
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
