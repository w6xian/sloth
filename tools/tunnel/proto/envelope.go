package proto

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/w6xian/sloth/v4/decoder/ag"
)

// ErrNotEnvelope 输入既不是隧道帧也不是 ws 推送的 JSON 信封。
var ErrNotEnvelope = errors.New("tunnel: not a tunnel frame or message envelope")

// UnwrapEnvelope 从 ws 推送通道的 JSON 信封里还原隧道帧。
//
// ws 数据面的推送在 wire 上是 message.Msg 的 JSON：{"type":1,"body":"<base64>"}，
// 两端的 handler.OnData 收到的都是这段 JSON 字节（见 examples/ai-stream 的
// stream_test.go 对同一行为的注释）。这里兼容三种入站形态：
//
//  1. 直接是隧道帧（首字节 Magic）：原样返回（TCP/QUIC 等裸传输、或框架
//     未来改动直传 body 时的兼容路径）；
//  2. JSON 信封，body 就是隧道帧：base64 解出即返回（relay ch.Push 直推）；
//  3. JSON 信封，body 外还套了一层 ag 帧（agent 用 client.Send，框架会先
//     经 ag.Encoder 包装）：base64 解出后再剥一层 ag。
func UnwrapEnvelope(b []byte) ([]byte, error) {
	if len(b) > 0 && b[0] == Magic {
		return b, nil
	}
	// client.Send 的路径：Send 先经 ag.Encoder 把隧道帧包成 ag 帧（magic ":p"），
	// 再作为 Msg.Body **裸发**（ws 客户端的 PSend 分支不套 JSON 信封）。
	// 服务端 OnData 收到的就是这层 ag 帧，剥掉即得隧道帧。
	if len(b) >= 2 && b[0] == ag.ArgumentMagic1 && b[1] == ag.ArgumentMagic2 {
		if v, err := ag.Decoder(b); err == nil && len(v) > 0 {
			return v, nil
		}
	}
	var env struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return nil, ErrNotEnvelope
	}
	if env.Body == "" {
		return nil, ErrNotEnvelope
	}
	raw, err := base64.StdEncoding.DecodeString(env.Body)
	if err != nil {
		return nil, ErrNotEnvelope
	}
	if len(raw) >= 2 && raw[0] == ag.ArgumentMagic1 && raw[1] == ag.ArgumentMagic2 {
		if v, err := ag.Decoder(raw); err == nil && len(v) > 0 {
			return v, nil
		}
	}
	return raw, nil
}
