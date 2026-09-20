//go:build !race

package codec

import (
	"testing"

	"github.com/w6xian/sloth/v4/actions"
)

// 本文件的测试要真分配 1GB+ 的内存块（为了触发"超过 FnMaxDataSize"那条分支）。
//
// 为什么单独放一个文件并排除 -race：ThreadSanitizer 需要一次性映射约 2GB
// 地址空间，在常规 CI 机器上会直接 "failed to allocate ... (error code: 1455)"
// 而挂掉整个包——这不是数据竞争，纯粹是 TSan 的地址空间限制。
// 用 build tag 隔离，保证 `go test -race ./...` 在 CI 上仍然可用。

// TestFnCodecEncodeOversize 超出 1GB 上限的数据应被拒绝。
func TestFnCodecEncodeOversize(t *testing.T) {
	co := UseCodec(CODEC_CODER_FN)
	big := make([]byte, (1<<30)+1) // 超过 FnMaxDataSize
	if _, err := co.Encode(actions.ACTION_CALL, 1, big); err == nil {
		t.Fatal("encode of oversized data should error")
	}
}
