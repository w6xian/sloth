package nrpc

import (
	"context"
	"testing"

	"github.com/w6xian/sloth/bucket"
	"github.com/w6xian/sloth/message"
)

// 测试IChannel实现
type TestChannel struct {
	auth *AuthInfo
}

func (c *TestChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	return nil, nil
}

func (c *TestChannel) Push(ctx context.Context, msg *message.Msg) error {
	return nil
}

func (c *TestChannel) GetAuthInfo() (*AuthInfo, error) {
	return c.auth, nil
}

func (c *TestChannel) SetAuthInfo(auth *AuthInfo) error {
	c.auth = auth
	return nil
}

// 测试IBucket实现
type TestBucket struct {
	rooms map[int64]*bucket.Room
}

func (b *TestBucket) Bucket(userId int64) *bucket.Bucket {
	return bucket.NewBucket()
}

func (b *TestBucket) Channel(userId int64) bucket.IChannel {
	return nil
}

func (b *TestBucket) Room(roomId int64) *bucket.Room {
	if b.rooms == nil {
		b.rooms = make(map[int64]*bucket.Room)
	}
	if room, ok := b.rooms[roomId]; ok {
		return room
	}
	room := bucket.NewRoom(roomId)
	b.rooms[roomId] = room
	return room
}

func (b *TestBucket) Broadcast(ctx context.Context, msg *message.Msg) error {
	return nil
}

// 测试RpcCaller
func TestRpcCaller(t *testing.T) {
	caller := &RpcCaller{
		Method:   "test.Hello",
		Data:     []byte("test data"),
		Args:     [][]byte{[]byte("arg1"), []byte("arg2")},
		Protocol: 1,
	}

	if caller.Method != "test.Hello" {
		t.Errorf("Expected method 'test.Hello', got '%s'", caller.Method)
	}

	if string(caller.Data) != "test data" {
		t.Errorf("Expected data 'test data', got '%s'", string(caller.Data))
	}

	if len(caller.Args) != 2 {
		t.Errorf("Expected 2 args, got %d", len(caller.Args))
	}

	if caller.Protocol != 1 {
		t.Errorf("Expected protocol 1, got %d", caller.Protocol)
	}
}

// 测试AuthInfo
func TestAuthInfo(t *testing.T) {
	auth := &AuthInfo{
		UserId: 1,
		RoomId: 100,
		Token:  "test-token",
	}

	if auth.UserId != 1 {
		t.Errorf("Expected userId 1, got %d", auth.UserId)
	}

	if auth.RoomId != 100 {
		t.Errorf("Expected roomId 100, got %d", auth.RoomId)
	}

	if auth.Token != "test-token" {
		t.Errorf("Expected token 'test-token', got '%s'", auth.Token)
	}
}

// 测试DefaultEncoder和DefaultDecoder
func TestDefaultEncoderDecoder(t *testing.T) {
	// 测试数据
	data := map[string]string{"key": "value"}

	// 测试编码
	encoded, err := DefaultEncoder(data)
	if err != nil {
		t.Errorf("DefaultEncoder should not return error, got %v", err)
	}

	if len(encoded) == 0 {
		t.Errorf("DefaultEncoder should return non-empty data")
	}

	// 测试解码
	decoded, err := DefaultDecoder(encoded)
	if err != nil {
		t.Errorf("DefaultDecoder should not return error, got %v", err)
	}

	if len(decoded) == 0 {
		t.Errorf("DefaultDecoder should return non-empty data")
	}
}
