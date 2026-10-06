package sloth

import (
	"strings"
	"testing"

	"github.com/w6xian/sloth/v4/decoder/ag"
)

// TestAgExtFrameEndToEnd 抬上限后，超过 64KB 的入参与回包能完整走完一次 RPC。
//
// ag 包的单测只证明编解码，这里证明的是**整条链路**：
// 入参编码 → fn 帧 → 传输 → 服务端解码 → 回包编码 → 回程解码。
// 任一层偷偷假设了"一帧不超过 64KB"，都会在这里暴露。
func TestAgExtFrameEndToEnd(t *testing.T) {
	// 收发两端都要抬；同一个进程里一次调用即可，跨进程部署时两边都得设
	ag.SetMaxDataSize(4 << 20)
	t.Cleanup(func() { ag.SetMaxDataSize(ag.ArgumentMaxDataSize) })

	ctx, cli := startTcpEnv(t)

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
