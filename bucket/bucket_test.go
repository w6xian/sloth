package bucket

import (
	"context"
	"testing"

	"github.com/w6xian/sloth/message"
)

// 测试通道实现
type TestChannel struct {
	userId int64
	room   *Room
	token  string
	prev   IChannel
	next   IChannel
}

func (c *TestChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	return nil, nil
}

func (c *TestChannel) Push(ctx context.Context, msg *message.Msg) error {
	return nil
}

func (c *TestChannel) ReplySuccess(id string, data []byte) error {
	return nil
}

func (c *TestChannel) ReplyError(id string, err []byte) error {
	return nil
}

func (c *TestChannel) Prev(p ...IChannel) IChannel {
	if len(p) > 0 {
		c.prev = p[0]
	}
	return c.prev
}

func (c *TestChannel) Next(n ...IChannel) IChannel {
	if len(n) > 0 {
		c.next = n[0]
	}
	return c.next
}

func (c *TestChannel) Room(r ...*Room) *Room {
	if len(r) > 0 {
		c.room = r[0]
	}
	return c.room
}

func (c *TestChannel) UserId(u ...int64) int64 {
	if len(u) > 0 {
		c.userId = u[0]
	}
	return c.userId
}

func (c *TestChannel) Token(t ...string) string {
	if len(t) > 0 {
		c.token = t[0]
	}
	return c.token
}

func (c *TestChannel) Close() error {
	return nil
}

// 测试Bucket创建
func TestNewBucket(t *testing.T) {
	b := NewBucket()

	if b == nil {
		t.Errorf("NewBucket should not return nil")
	}

	if b.ChannelSize != 1024 {
		t.Errorf("Expected ChannelSize 1024, got %d", b.ChannelSize)
	}

	if b.RoomSize != 1024 {
		t.Errorf("Expected RoomSize 1024, got %d", b.RoomSize)
	}

	if b.RoutineAmount != 32 {
		t.Errorf("Expected RoutineAmount 32, got %d", b.RoutineAmount)
	}

	if b.RoutineSize != 20 {
		t.Errorf("Expected RoutineSize 20, got %d", b.RoutineSize)
	}
}

// 测试Put和Channel
func TestBucketPutChannel(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// 测试Put
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	// 测试Channel
	ch2 := b.Channel(1)
	if ch2 == nil {
		t.Errorf("Channel should not return nil")
	}

	if ch2.UserId() != 1 {
		t.Errorf("Expected userId 1, got %d", ch2.UserId())
	}

	// 测试Room
	room := b.Room(100)
	if room == nil {
		t.Errorf("Room should not return nil")
	}

	if room.Id != 100 {
		t.Errorf("Expected roomId 100, got %d", room.Id)
	}
}

// 测试更新连接
func TestBucketUpdateChannel(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// 第一次Put
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	// 第二次Put，更新token
	err = b.Put(1, 100, "token2", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	// 测试Channel
	ch2 := b.Channel(1)
	if ch2 == nil {
		t.Errorf("Channel should not return nil")
	}

	if ch2.Token() != "token2" {
		t.Errorf("Expected token 'token2', got '%s'", ch2.Token())
	}
}

// 测试切换房间
func TestBucketSwitchRoom(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// 第一次Put到房间100
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	if ch.Room().Id != 100 {
		t.Errorf("Expected roomId 100, got %d", ch.Room().Id)
	}

	// 第二次Put到房间200
	err = b.Put(1, 200, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	if ch.Room().Id != 200 {
		t.Errorf("Expected roomId 200, got %d", ch.Room().Id)
	}

	// 测试房间100是否存在
	room100 := b.Room(100)
	if room100 == nil {
		t.Errorf("Room 100 should not be nil")
	}

	// 测试房间200是否存在
	room200 := b.Room(200)
	if room200 == nil {
		t.Errorf("Room 200 should not be nil")
	}
}

// 测试Quit
func TestBucketQuit(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// Put到房间100
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	if ch.Room().Id != 100 {
		t.Errorf("Expected roomId 100, got %d", ch.Room().Id)
	}

	// Quit
	err = b.Quit(ch)
	if err != nil {
		t.Errorf("Quit should not return error, got %v", err)
	}

	if ch.Room().Id != Plaza {
		t.Errorf("Expected roomId Plaza, got %d", ch.Room().Id)
	}
}

// 测试DeleteChannel
func TestBucketDeleteChannel(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// Put
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	// 测试Channel存在
	ch2 := b.Channel(1)
	if ch2 == nil {
		t.Errorf("Channel should not return nil")
	}

	// DeleteChannel
	b.DeleteChannel(ch)

	// 测试Channel不存在
	ch3 := b.Channel(1)
	if ch3 != nil {
		t.Errorf("Channel should return nil after DeleteChannel")
	}
}

// 测试BroadcastRoom
func TestBucketBroadcastRoom(t *testing.T) {
	b := NewBucket()

	// 创建测试通道
	ch := &TestChannel{}

	// Put
	err := b.Put(1, 100, "token1", ch)
	if err != nil {
		t.Errorf("Put should not return error, got %v", err)
	}

	// 创建广播消息
	msg := &message.PushRoomMsgRequest{
		RoomId: 100,
		Msg:    &message.Msg{Type: message.TextMessage, Body: []byte("test message")},
	}

	// 测试BroadcastRoom
	b.BroadcastRoom(msg)

	// 这里我们只能测试方法不会 panic，因为消息处理在 goroutine 中
}

// 测试Room功能
func TestRoom(t *testing.T) {
	room := NewRoom(100)

	if room == nil {
		t.Errorf("NewRoom should not return nil")
	}

	if room.Id != 100 {
		t.Errorf("Expected roomId 100, got %d", room.Id)
	}

	// 创建测试通道
	ch := &TestChannel{}

	// 测试Put
	err := room.Put(ch)
	if err != nil {
		t.Errorf("Room.Put should not return error, got %v", err)
	}

	// 测试DeleteChannel
	empty := room.DeleteChannel(ch)
	if !empty {
		t.Errorf("Expected room to be empty after DeleteChannel")
	}

	if !room.Drop {
		t.Errorf("Expected room.Drop to be true after DeleteChannel")
	}
}
