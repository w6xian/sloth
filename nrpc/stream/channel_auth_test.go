package stream

import (
	"net"
	"testing"

	"github.com/w6xian/sloth/v4/types/auth"
)

// TestChannelLocalAuth 客户端侧身份：ws 客户端的连接自带身份，
// stream 连接（tcp / quic）必须有等价能力。
//
// 少了它，同一个客户端方法（服务端反调进来的那种）在 ws 上能读到 userId，
// 在 tcp / quic 上只能拿到 "user id is 0"——换传输就换行为。
func TestChannelLocalAuth(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	ch := NewChannel(nil, c, "127.0.0.1", 1)

	// 未设置身份时必须有错：与服务端语义一致（没登录就是没身份）
	if _, err := ch.GetAuthInfo(); err == nil {
		t.Fatal("GetAuthInfo before auth should error")
	}

	want := &auth.AuthInfo{UserId: 7, RoomId: 9, Token: "tk"}
	if err := ch.SetLocalAuth(want); err != nil {
		t.Fatalf("SetLocalAuth err: %v", err)
	}
	got, err := ch.GetAuthInfo()
	if err != nil {
		t.Fatalf("GetAuthInfo err: %v", err)
	}
	if got.UserId != 7 || got.RoomId != 9 || got.Token != "tk" {
		t.Fatalf("auth mismatch: %+v", got)
	}

	// 存的是副本：调用方之后再改自己的 AuthInfo 不应反向影响连接
	want.UserId = 100
	if got2, _ := ch.GetAuthInfo(); got2.UserId != 7 {
		t.Fatalf("local auth aliased caller's struct: userId=%d", got2.UserId)
	}

	if err := ch.SetLocalAuth(nil); err == nil {
		t.Fatal("SetLocalAuth(nil) should error")
	}
}

// TestChannelServerAuthUnaffected 写入客户端身份不影响服务端语义那套字段。
func TestChannelServerAuthUnaffected(t *testing.T) {
	c, s := net.Pipe()
	defer c.Close()
	defer s.Close()
	ch := NewChannel(nil, c, "127.0.0.1", 1)

	_ = ch.SetLocalAuth(&auth.AuthInfo{UserId: 7, RoomId: 9, Token: "tk"})
	// _userId / _sign 属于服务端语义（bucket.Put 写入），客户端身份不该污染它们
	if ch.UserId() != 0 || ch.Token() != "" {
		t.Fatalf("local auth leaked into server-side fields: userId=%d token=%q",
			ch.UserId(), ch.Token())
	}
}
