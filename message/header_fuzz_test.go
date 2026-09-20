package message

import "testing"

// FuzzNewHeaderFromBV TLV 字节流 → Header。
//
// 该函数是唯一直接吃不可信字节并调用外部 tlv 库的地方：tlv.JsonUnpack 对畸形
// 输入会 panic（slice 越界），函数内已用 recover 转成 error。用 fuzz 守住这条
// 边界——一旦 tlv 库改动或新增解析分支漏了保护，这里会立刻暴露。
func FuzzNewHeaderFromBV(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{0x00})
	f.Add([]byte{0x01, 0x02, 0x03, 0x04})
	f.Add([]byte(`{"APP_ID":"1","USER_ID":"2"}`))
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	f.Fuzz(func(t *testing.T, bv []byte) {
		h, err := NewHeaderFromBV(bv)
		if err != nil {
			if h != nil {
				t.Fatalf("NewHeaderFromBV(%x) 返回 error 却带回了非 nil header", bv)
			}
			return
		}
		// 解码成功后读一遍，确保 header 自身的方法对任意内容都安全
		_ = h.Get("APP_ID")
	})
}
