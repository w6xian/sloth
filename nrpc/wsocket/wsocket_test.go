package wsocket

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/message"
	"github.com/w6xian/sloth/v3/types/auth"
)

// TestGetBucketStableAndBoundary 分桶一致性：同一 userId 必须恒定命中同一分片。
func TestGetBucketStableAndBoundary(t *testing.T) {
	buckets := []*bucket.Bucket{{}, {}, {}}
	ctx := context.Background()
	for id := int64(0); id < 1000; id++ {
		b := GetBucket(ctx, buckets, id)
		if b != GetBucket(ctx, buckets, id) {
			t.Fatalf("GetBucket unstable for id %d", id)
		}
		// 同一 bucket 被赋值给 id
		for i, cand := range buckets {
			if b == cand {
				break
			}
			if i == len(buckets)-1 {
				t.Fatalf("GetBucket returned a bucket not in the pool for id %d", id)
			}
		}
	}
	// 单分片场景恒命中唯一分片
	one := []*bucket.Bucket{bucket.NewBucket()}
	for id := int64(1); id < 500; id += 7 {
		if got := GetBucket(ctx, one, id); got != one[0] {
			t.Fatalf("single bucket should always be returned, id=%d", id)
		}
	}
}

// TestGetSliceNameWrap 分片名在 0..99 之间循环且格式 %02d。
func TestGetSliceNameWrap(t *testing.T) {
	atomic.StoreInt32(&ids, 97)
	for want := int32(98); want <= 99; want++ {
		if got := getSliceName(); got != string(rune('0'+want/10))+string(rune('0'+want%10)) {
			t.Fatalf("getSliceName want %02d, got %s", want, got)
		}
	}
	// 越过 99 后回绕到 00
	if got := getSliceName(); got != "00" {
		t.Fatalf("getSliceName should wrap to 00, got %s", got)
	}
	if got := getSliceName(); got != "01" {
		t.Fatalf("getSliceName should continue to 01, got %s", got)
	}
}

func newTestChannelServer(t *testing.T) *WsChannelServer {
	ch := NewWsChannelServer(nil)
	t.Cleanup(func() { _ = ch.Close() })
	return ch
}

func newTestChannelClient(t *testing.T) *WsChannelClient {
	c := NewWsChannelClient(nil)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// TestChannelServerAuthFlow 服务端通道的认证状态流转（登录/获取/登出）。
func TestChannelServerAuthFlow(t *testing.T) {
	ch := newTestChannelServer(t)
	if _, err := ch.GetAuthInfo(); err == nil {
		t.Fatal("GetAuthInfo before login should error")
	}
	room := bucket.NewRoom(100)
	if err := room.Join(ch); err != nil {
		t.Fatalf("Join room err: %v", err)
	}
	ch.Room(room)
	ch.UserId(7)
	ch.Token("s3cret")
	info, err := ch.GetAuthInfo()
	if err != nil {
		t.Fatalf("GetAuthInfo err: %v", err)
	}
	if info.UserId != 7 || info.RoomId != 100 || info.Token != "s3cret" {
		t.Fatalf("auth mismatch: %+v", info)
	}
	if err := ch.SetAuthInfo(info); err == nil {
		t.Fatal("server channel SetAuthInfo should be unsupported")
	}
	ch.Logout()
	if _, err := ch.GetAuthInfo(); err == nil {
		t.Fatal("GetAuthInfo after logout should error")
	}
}

// TestChannelServerCloseConcurrent 并发 Close 不得 panic、done 只关闭一次。
func TestChannelServerCloseConcurrent(t *testing.T) {
	ch := NewWsChannelServer(nil)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ch.Close(); err != nil {
				t.Errorf("Close err: %v", err)
			}
		}()
	}
	wg.Wait()
	select {
	case _, ok := <-ch.done:
		if ok {
			t.Fatal("done channel should be closed")
		}
	default:
		t.Fatal("done channel not closed after Close")
	}
}

// TestChannelServerPushQueueFull 广播队列（cap=10）打满后 Push 应因 ctx 超时返回错误。
func TestChannelServerPushQueueFull(t *testing.T) {
	ch := newTestChannelServer(t)
	msg := message.NewTextMessage([]byte("hello"))
	for i := 0; i < 10; i++ {
		if err := ch.Push(context.Background(), msg); err != nil {
			t.Fatalf("push %d should succeed, err: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := ch.Push(ctx, msg); err == nil {
		t.Fatal("push beyond capacity should return ctx timeout error")
	}
	// ctx 提前取消同样返回 ctx 错误
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if err := ch.Push(ctx2, msg); err == nil {
		t.Fatal("push with cancelled ctx should error")
	}
}

// TestChannelClientAuthAndClose 客户端通道认证存取与 Close 幂等。
func TestChannelClientAuthAndClose(t *testing.T) {
	c := newTestChannelClient(t)
	if err := c.SetAuthInfo(nil); err == nil {
		t.Fatal("SetAuthInfo(nil) should error")
	}
	info := &auth.AuthInfo{UserId: 3, RoomId: 9, Token: "tk"}
	if err := c.SetAuthInfo(info); err != nil {
		t.Fatalf("SetAuthInfo err: %v", err)
	}
	got, err := c.GetAuthInfo()
	if err != nil {
		t.Fatalf("GetAuthInfo err: %v", err)
	}
	if got.UserId != 3 || got.RoomId != 9 || got.Token != "tk" {
		t.Fatalf("auth mismatch: %+v", got)
	}
	if c.GetUserId() != 3 || c.GetRoomId() != 9 {
		t.Fatalf("GetUserId/GetRoomId mismatch: %d/%d", c.GetUserId(), c.GetRoomId())
	}
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout err: %v", err)
	}
	if c.GetUserId() != 0 || c.GetRoomId() != 0 || c.Sign != "" {
		t.Fatal("Logout should clear auth state")
	}
	// Close 幂等：多次调用不报错不 panic
	for i := 0; i < 3; i++ {
		if err := c.Close(); err != nil {
			t.Fatalf("Close #%d err: %v", i, err)
		}
	}
	if c.Conn != nil {
		t.Fatal("Conn should be nil after Close")
	}
}

// TestChannelClientCloseConcurrent 并发 Close 不得 panic（回归：closeOnce 竞态）。
func TestChannelClientCloseConcurrent(t *testing.T) {
	c := NewWsChannelClient(nil)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.Close(); err != nil {
				t.Errorf("Close err: %v", err)
			}
		}()
	}
	wg.Wait()
	if c.Conn != nil {
		t.Fatal("Conn should be nil after Close")
	}
}

// TestChannelClientPushQueueFull 客户端发送队列（cap=5）打满后 Push 返回 ctx 错误。
func TestChannelClientPushQueueFull(t *testing.T) {
	c := newTestChannelClient(t)
	msg := message.NewTextMessage([]byte("hi"))
	for i := 0; i < 5; i++ {
		if err := c.Push(context.Background(), msg); err != nil {
			t.Fatalf("push %d should succeed, err: %v", i, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := c.Push(ctx, msg); err == nil {
		t.Fatal("push beyond capacity should return ctx timeout error")
	}
}
