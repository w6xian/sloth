package frame

import "testing"

// FuzzDecode 用任意字节喂分片解码器。
//
// 分片头来自网络，恶意/畸形帧必须能被安全地拒绝（返回 error），而不是 panic
// 或直接越界读内存。历史上有两个真实越界口子都在这个函数里：
//   1. 按最小头长(7)校验后去读 b[5:9]（4 字节长度字段）→ 7~8 字节的帧越界；
//   2. 总长校验用 int 相加，uint32 长度在 32 位平台溢出成负数 → 校验被绕过。
// 这两个都已修，用 fuzz 守住回归。
func FuzzDecode(f *testing.F) {
	seeds := [][]byte{
		{},
		{0x01, 'a', 'b', 1, 0, 0, 2, 'h', 'i'},           // 正常短帧
		{0x01, 'a', 'b', 1, 0, 0, 2},                     // 声明长度 2 但无数据
		{0x81, 'a', 'b', 1, 0, 0},                        // 长帧标记 + 长度不足（原越界点）
		{0x81, 'a', 'b', 1, 0, 0, 0},                     // 长帧标记 + 7 字节（原越界点）
		{0x81, 'a', 'b', 1, 0, 0, 0, 0, 1, 'x'},          // 长帧正常
		{0xC1, 'a', 'b', 1, 0, 0, 2, 0, 0, 'y', 'z'},     // CRC 标记
		{0x81, 'a', 'b', 1, 0xFF, 0xFF, 0xFF, 0xFF, 'z'}, // 声明长度接近 uint32 上限
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s, err := Decode(b)
		if err != nil {
			if s != nil {
				t.Fatalf("Decode(%x) 返回 error 却带回了非 nil 分片", b)
			}
			return
		}
		if s == nil {
			t.Fatalf("Decode(%x) 返回 nil error 但分片为 nil", b)
		}
		// 解码成功时 D 必须就是声明长度的那一段，不能多读也不能少读
		if uint32(len(s.D)) != s.S {
			t.Fatalf("Decode(%x): len(D)=%d 与声明长度 S=%d 不一致", b, len(s.D), s.S)
		}
	})
}

// FuzzFromType 覆盖按消息类型分发的解码入口（JSON 文本帧 / 二进制帧两条路径）。
func FuzzFromType(f *testing.F) {
	f.Add([]byte(`{"p":1,"n":"ab","t":1,"i":0,"s":2,"d":"aGk="}`), byte(TextMessage))
	f.Add([]byte{0x01, 'a', 'b', 1, 0, 0, 2, 'h', 'i'}, byte(BinaryMessage))
	f.Add([]byte{0x81}, byte(BinaryMessage))
	f.Add([]byte(nil), byte(TextMessage))
	f.Fuzz(func(t *testing.T, b []byte, messageType byte) {
		// 只要求：任意输入都不 panic，且 error 与返回值成对
		s, err := FromType(b, messageType)
		if err != nil && s != nil {
			t.Fatalf("FromType(%x,%d) 返回 error 却带回了非 nil 分片", b, messageType)
		}
		if err == nil && s == nil {
			t.Fatalf("FromType(%x,%d) 返回 nil error 但分片为 nil", b, messageType)
		}
	})
}
