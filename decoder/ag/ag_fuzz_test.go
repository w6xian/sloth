package ag

import "testing"

// FuzzDecode AG 参数帧解码：输入完全来自网络，任意字节都必须安全返回 error。
//
// 重点守住长度字段（2 字节 uint16）与 Value 段的一致性：历史上 Encode 侧把上限
// 写成 1<<16（65536），会被 PutUint16 截断成 0，编出长度 0 的帧导致数据静默丢失。
// 解码侧同样不能越界读。
func FuzzDecode(f *testing.F) {
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeString, 0, 2, 'h', 'i'})
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeInt64, 0, 0})
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeBytes, 0xFF, 0xFF})
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, 0x7F, 0, 1, 'x'}) // 未知类型 tag
	f.Add([]byte{ArgumentMagic1})
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, b []byte) {
		// 契约：绝不 panic；返回 error 时不保证返回值，返回 nil error 时值必须可用。
		if _, err := Decode(b); err != nil {
			return
		}
		// 解码成功后再取一次 Data 段，确保该辅助函数对同一输入同样不 panic
		_ = Data(b)
	})
}

// FuzzValidate 覆盖合法性判定入口：它是解码前的快速筛子，
// 任何输入都必须返回 bool 而不是 panic。
func FuzzValidate(f *testing.F) {
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeString, 0, 2, 'h', 'i'})
	f.Add([]byte{ArgumentMagic1, ArgumentMagic2, ArgumentTypeString, 0, 9})
	f.Add([]byte(nil))
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = Validate(b)
	})
}
