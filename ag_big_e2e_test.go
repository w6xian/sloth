package sloth

import (
	"errors"
	"strings"
	"testing"

	"github.com/w6xian/sloth/v4/decoder/ag"
)

// TestAgExtFrameEndToEnd 超过 64KB 的入参与回包能完整走完一次 RPC。
//
// ag 包的单测只证明编解码，这里证明的是**整条链路**：
// 入参编码 → fn 帧 → 传输 → 服务端解码 → 回包编码 → 回程解码。
// 任一层偷偷假设了"一帧不超过 64KB"，都会在这里暴露。
//
// 这里**不调任何全局开关**：限制是连接级的部署策略（WithMaxParamSize），
// 协议本身就能表达大帧，不该要求业务为了传大包去改进程级状态。
func TestAgExtFrameEndToEnd(t *testing.T) {
	ctx, cli := startTcpEnv(t, WithMaxParamSize(4<<20))

	payload := strings.Repeat("x", 1<<20) // 1MB，远超旧的 65535 上限
	resp, err := cli.server.Call(ctx, "v1.Echo", payload)
	if err != nil {
		t.Fatalf("call Echo with 1MB: %v", err)
	}
	if len(resp) != len(payload) {
		t.Fatalf("resp len = %d, want %d", len(resp), len(payload))
	}
	if string(resp) != payload {
		t.Fatalf("resp content mismatch at first diff: %d", firstDiff(string(resp), payload))
	}
}

// TestMaxParamSizeRejectsOversize 限制收紧后，超限的入参在**出站编码阶段**就被拒，
// 不会发出去让对端拒——更不会按"声明长度"分配内存。
func TestMaxParamSizeRejectsOversize(t *testing.T) {
	ctx, cli := startTcpEnv(t, WithMaxParamSize(1024))

	_, err := cli.server.Call(ctx, "v1.Echo", strings.Repeat("x", 4096))
	if !errors.Is(err, ag.ErrAgDataTooLarge) {
		t.Fatalf("oversized arg err=%v, want ErrAgDataTooLarge", err)
	}

	// 限制内的照常通
	resp, err := cli.server.Call(ctx, "v1.Echo", strings.Repeat("x", 512))
	if err != nil {
		t.Fatalf("call Echo with 512B: %v", err)
	}
	if len(resp) != 512 {
		t.Fatalf("resp len = %d, want 512", len(resp))
	}
}

// TestMaxParamSizeSyncsBothDirections WithMaxParamSize 只传给一侧时，
// 另一侧（入站解码 / 出站编码）也必须生效——两个 rpc 对象在建连接时同步。
func TestMaxParamSizeSyncsBothDirections(t *testing.T) {
	// 只给 DefaultClient（出站侧）设，服务端的入站解码要跟着收紧
	ctx, cli := startTcpEnv(t, WithMaxParamSize(1024))

	if cli.server.MaxParamSize() != 1024 {
		t.Fatalf("outbound maxParamSize=%d, want 1024", cli.server.MaxParamSize())
	}
	if cli.client.MaxParamSize() != 1024 {
		t.Fatalf("inbound side not synced: maxParamSize=%d, want 1024", cli.client.MaxParamSize())
	}
	// 未设限制的默认值是 0（走 ag 的进程级默认）
	plain := DefaultServer()
	if plain.MaxParamSize() != 0 {
		t.Fatalf("unset maxParamSize=%d, want 0", plain.MaxParamSize())
	}
	_ = ctx
}

func firstDiff(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}
