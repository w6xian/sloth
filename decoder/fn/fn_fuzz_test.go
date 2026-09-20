package fn

import "testing"

// FuzzDecodeFn FN 帧解码：FN 帧承载 RPC 的 action/id/payload，是每次调用都要
// 解析的热路径，且完全由对端字节决定。这里守住"任意输入不 panic"。
func FuzzDecodeFn(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{FnMagic1, FnMagic2})
	f.Add(make([]byte, FnHeaderSize))
	f.Add([]byte{FnMagic1, FnMagic2, 0x01, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, b []byte) {
		frame, err := DecodeFn(b)
		if err != nil {
			if frame != nil {
				t.Fatalf("DecodeFn(%x) 返回 error 却带回了非 nil 帧", b)
			}
			return
		}
		if frame == nil {
			t.Fatalf("DecodeFn(%x) 返回 nil error 但帧为 nil", b)
		}
	})
}

// FuzzDecode 覆盖一步到位的 action/id/data 解码入口。
func FuzzDecode(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{FnMagic1, FnMagic2, 0x01, 0, 0, 0, 0, 0, 0, 0})
	f.Add([]byte{FnMagic1, FnMagic2, 0x02, 0, 0, 0, 0, 0, 0, 5, 'h', 'e', 'l', 'l', 'o'})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _, _, err := Decode(b)
		if err != nil {
			return
		}
		// 解码成功后，同一输入再用各取值函数读一遍：
		// Action/Id/Data 会对 b 直接索引，短帧或长度不符时最容易越界。
		_, _ = Action(b)
		_ = Id(b)
		_ = Data(b)
	})
}
