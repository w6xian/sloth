package wsocket

import (
	"testing"

	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/option"
)

// stubCodec 只认以 'X' 开头的帧。
type stubCodec struct{}

func (stubCodec) Detect(raw []byte) bool { return len(raw) > 0 && raw[0] == 'X' }
func (stubCodec) Decode(raw []byte) (uint8, uint64, []byte, error) {
	return 1, 1, raw, nil
}
func (stubCodec) Encode(action uint8, id uint64, data []byte) ([]byte, error) {
	return data, nil
}

// TestWsServerCodecInjection option.WithCodec 必须能作用到 WsServer 上。
//
// 回归防护：这条链路此前是断的——codec 被存进 RpcConn.Codec 之后没有任何
// 地方读取它（dispatch 不传），用户设了自定义 codec 却不生效、也看不出来。
// 现在 dispatch 会把它交给 DispatchMessage，这里守住"能存进来"这一环。
func TestWsServerCodecInjection(t *testing.T) {
	// 编译期约束：WsServer 必须满足 option 接口，否则 NewWsServer 里的
	// "for _, opt := range opts { opt(s) }" 根本编译不过。
	var s WsServer
	var _ option.IConnectOption = &s

	want := stubCodec{}
	option.WithCodec(want)(&s)
	if s.Codec == nil {
		t.Fatal("option.WithCodec 没有写进 WsServer（codec 注入链路断了）")
	}
	got, ok := s.Codec.(stubCodec)
	if !ok {
		t.Fatalf("WsServer.Codec = %T, want stubCodec", s.Codec)
	}
	if !got.Detect([]byte("Xok")) || got.Detect([]byte("@F")) {
		t.Fatal("取回的 codec 行为不符")
	}

	// 未注入时保持 nil：dispatch 会退化为按帧内容自动识别（codec.Select）
	var s2 WsServer
	if s2.Codec != nil {
		t.Fatalf("默认 codec = %v, want nil", s2.Codec)
	}
}

// TestSelectSingleVerdict codec 包的帧识别入口与 fn 包判定一致。
func TestSelectSingleVerdict(t *testing.T) {
	frame, err := codec.UseCodec(codec.CODEC_CODER_FN).Encode(1, 7, []byte("body"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, ok := codec.Select(frame); !ok {
		t.Fatal("Select 应识别 FN 帧")
	}
	if _, ok := codec.Select([]byte("plain")); ok {
		t.Fatal("Select 不应把裸数据识别为 FN 帧")
	}
}
