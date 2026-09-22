package transport

import (
	"context"
	"testing"

	"github.com/w6xian/sloth/v4/decoder/fn"
	"github.com/w6xian/sloth/v4/codec"
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

func route() (fnHit, dataHit *int, onFn, onData RouteHandler) {
	var f, d int
	onFn = func(ctx context.Context, raw []byte) error { f++; return nil }
	onData = func(ctx context.Context, raw []byte) error { d++; return nil }
	return &f, &d, onFn, onData
}

// TestFrameRouterDefaultCodec 未注入 codec 时按帧内容自动识别：
// FN 帧走 OnFn，其余走 OnData（识别入口是 codec.Select）。
func TestFrameRouterDefaultCodec(t *testing.T) {
	f, d, onFn, onData := route()
	r := NewFrameRouter(onFn, onData)

	frame, err := fn.Encode(1, 7, []byte("body"))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := r.Dispatch(context.Background(), frame); err != nil {
		t.Fatalf("dispatch fn frame: %v", err)
	}
	if *f != 1 || *d != 0 {
		t.Fatalf("FN frame: fn=%d data=%d, want 1/0", *f, *d)
	}
	if err := r.Dispatch(context.Background(), []byte("plain business data")); err != nil {
		t.Fatalf("dispatch data: %v", err)
	}
	if *f != 1 || *d != 1 {
		t.Fatalf("plain data: fn=%d data=%d, want 1/1", *f, *d)
	}
	// nil 帧必须被忽略（不能当成业务数据下发）
	if err := r.Dispatch(context.Background(), nil); err != nil {
		t.Fatalf("dispatch nil: %v", err)
	}
	if *f != 1 || *d != 1 {
		t.Fatalf("nil frame: fn=%d data=%d, want 1/1", *f, *d)
	}
}

// TestFrameRouterInjectedCodec 注入的 codec 必须真正参与帧判定。
//
// 这是 option.WithCodec 链路的最后一环：此前 codec 被存进 RpcConn.Codec 后
// 没有任何地方读取它，"可插拔 codec" 是条断头路——用户设了却不生效。
func TestFrameRouterInjectedCodec(t *testing.T) {
	f, d, onFn, onData := route()
	r := NewFrameRouterWithCodec(onFn, onData, stubCodec{})

	// 自定义 codec 认的帧：即便它不是 FN 帧也要走 OnFn
	if err := r.Dispatch(context.Background(), []byte("Xcustom")); err != nil {
		t.Fatalf("dispatch custom: %v", err)
	}
	if *f != 1 || *d != 0 {
		t.Fatalf("custom frame: fn=%d data=%d, want 1/0（注入的 codec 未生效）", *f, *d)
	}
	// 自定义 codec 不认 FN 帧 → 走 OnData（注入即接管判定）
	frame, _ := fn.Encode(1, 7, []byte("body"))
	if err := r.Dispatch(context.Background(), frame); err != nil {
		t.Fatalf("dispatch fn frame: %v", err)
	}
	if *f != 1 || *d != 1 {
		t.Fatalf("fn frame with injected codec: fn=%d data=%d, want 1/1", *f, *d)
	}
}

// TestSelectIsSingleVerdict 帧识别只有 Select 一个入口，且与 fn 包的判定一致。
func TestSelectIsSingleVerdict(t *testing.T) {
	good, _ := fn.Encode(1, 7, []byte("body"))
	cases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"fn frame", good, true},
		{"plain text", []byte("hello"), false},
		{"too short", []byte{0x40}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			co, ok := codec.Select(c.raw)
			if ok != c.want {
				t.Fatalf("Select ok = %v, want %v", ok, c.want)
			}
			if _, err := codec.GetCodecer(c.raw); (err == nil) != c.want {
				t.Fatalf("GetCodecer err = %v, want ok=%v", err, c.want)
			}
			if ok && co == nil {
				t.Fatal("Select returned ok but nil codec")
			}
		})
	}
}
