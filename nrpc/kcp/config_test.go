package kcp

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/option"
)

// fakeConn 同时满足 net.Conn 与 session：记录 tune 实际调用了哪些方法。
//
// 用它而不是真连接，是因为要断言的是"哪些 Set 被跳过"——
// 真连接上调错了参数不会报错，只会让连接卡死，测不出来。
type fakeConn struct {
	noDelay  []int
	win      []int
	mtuSet   int
	stream   bool
	ackDelay bool
	calls    []string
}

func (f *fakeConn) SetNoDelay(nodelay, interval, resend, nc int) {
	f.noDelay = []int{nodelay, interval, resend, nc}
	f.calls = append(f.calls, "nodelay")
}
func (f *fakeConn) SetWindowSize(sndwnd, rcvwnd int) {
	f.win = []int{sndwnd, rcvwnd}
	f.calls = append(f.calls, "wnd")
}
func (f *fakeConn) SetMtu(mtu int) bool {
	f.mtuSet = mtu
	f.calls = append(f.calls, "mtu")
	return true
}
func (f *fakeConn) SetStreamMode(enable bool) {
	f.stream = enable
	f.calls = append(f.calls, "stream")
}
func (f *fakeConn) SetACKNoDelay(nodelay bool) {
	f.ackDelay = nodelay
	f.calls = append(f.calls, "ack")
}

func (f *fakeConn) Read(p []byte) (int, error)         { return 0, io.EOF }
func (f *fakeConn) Write(p []byte) (int, error)        { return len(p), nil }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return nil }
func (f *fakeConn) RemoteAddr() net.Addr               { return nil }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }

// TestTuneZeroConfigIsNoop 零值配置不得改动任何参数。
//
// 这是"0 表示保持 kcp-go 默认"这条约定的回归测试：一旦哪天改成无条件调用
// SetWindowSize(0,0)，连接会直接卡死，而编译和大部分测试都不会报错。
func TestTuneZeroConfigIsNoop(t *testing.T) {
	c := &fakeConn{}
	tune(c, option.KCPConfig{})
	if len(c.calls) != 0 {
		t.Fatalf("zero config calls = %v, want none", c.calls)
	}
}

// TestTuneNodelayNeedsInterval 只开 nodelay 而不给 interval 时必须整体跳过：
// interval=0 会让 KCP 每收到一个包就 flush，CPU 空转。
func TestTuneNodelayNeedsInterval(t *testing.T) {
	c := &fakeConn{}
	tune(c, option.KCPConfig{NoDelay: 1})
	if len(c.calls) != 0 {
		t.Fatalf("nodelay without interval calls = %v, want none", c.calls)
	}

	c2 := &fakeConn{}
	tune(c2, option.KCPConfig{NoDelay: 1, Interval: 10, Resend: 2, NC: 1})
	if len(c2.noDelay) != 4 || c2.noDelay[1] != 10 {
		t.Fatalf("SetNoDelay args = %v, want interval 10", c2.noDelay)
	}
}

// TestTuneWindowNeverZero 只给一半窗口参数时，另一半必须按默认值补齐。
//
// SetWindowSize(0, x) 会把发送窗口设成 0，连接再也发不出数据——
// 这类错误在真连接上表现为"偶发卡住"，极难定位。
func TestTuneWindowNeverZero(t *testing.T) {
	c := &fakeConn{}
	tune(c, option.KCPConfig{SndWnd: 128})
	if len(c.win) != 2 || c.win[0] != 128 || c.win[1] != defaultRcvWnd {
		t.Fatalf("SetWindowSize args = %v, want [128 %d]", c.win, defaultRcvWnd)
	}

	c2 := &fakeConn{}
	tune(c2, option.KCPConfig{RcvWnd: 64})
	if c2.win[0] != defaultSndWnd || c2.win[1] != 64 {
		t.Fatalf("SetWindowSize args = %v, want [%d 64]", c2.win, defaultSndWnd)
	}
}

// TestTuneNonKcpConn 不是 KCP 连接时 tune 必须静默返回（不 panic）。
func TestTuneNonKcpConn(t *testing.T) {
	tune(plainConn{}, option.FastKCPConfig())
}

type plainConn struct{}

func (plainConn) Read(p []byte) (int, error)         { return 0, io.EOF }
func (plainConn) Write(p []byte) (int, error)        { return len(p), nil }
func (plainConn) Close() error                       { return nil }
func (plainConn) LocalAddr() net.Addr                { return nil }
func (plainConn) RemoteAddr() net.Addr               { return nil }
func (plainConn) SetDeadline(t time.Time) error      { return nil }
func (plainConn) SetReadDeadline(t time.Time) error  { return nil }
func (plainConn) SetWriteDeadline(t time.Time) error { return nil }

// TestNewBlockCrypt 各种加密方式的构造，以及未知值必须报错。
func TestNewBlockCrypt(t *testing.T) {
	key16 := []byte("0123456789abcdef")
	cases := []struct {
		crypt string
		key   []byte
		ok    bool
	}{
		{"", nil, true}, // 空串按 none 处理
		{option.KCPCryptNone, nil, true},
		{option.KCPCryptXOR, key16, true},
		{option.KCPCryptTEA, key16, true},
		{option.KCPCryptAES, key16, true},
		{option.KCPCryptBlowfish, key16, true},
		{option.KCPCryptSalsa20, append(key16[:], key16...), true}, // 需 32 字节
		{option.KCPCryptSM4, key16, true},
		{"rot13", key16, false},
	}
	for _, c := range cases {
		bc, err := newBlockCrypt(option.KCPConfig{Crypt: c.crypt, Key: c.key})
		if c.ok && err != nil {
			t.Errorf("crypt %q: unexpected err %v", c.crypt, err)
		}
		if !c.ok && err == nil {
			t.Errorf("crypt %q: want error, got %v", c.crypt, bc)
		}
	}
}

// TestNewBlockCryptShortKey AES 密钥长度不足必须报错，而不是建出一个坏连接。
func TestNewBlockCryptShortKey(t *testing.T) {
	if _, err := newBlockCrypt(option.KCPConfig{
		Crypt: option.KCPCryptAES,
		Key:   []byte("short"),
	}); err == nil {
		t.Fatal("aes with 5-byte key should error")
	}
}
