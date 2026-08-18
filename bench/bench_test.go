package bench

import (
	"context"
	"testing"

	"github.com/w6xian/sloth/v3/bucket"
	"github.com/w6xian/sloth/v3/decoder/ag"
	"github.com/w6xian/sloth/v3/decoder/frame"
	"github.com/w6xian/sloth/v3/message"
)

// mockChannel 最小化实现 bucket.IChannel，用于 Room benchmark。
type mockChannel struct {
	id   int64
	push func(ctx context.Context, msg *message.Msg) error
}

func (m *mockChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	return nil, nil
}
func (m *mockChannel) SendData(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockChannel) Send(payload []byte) error                { return nil }
func (m *mockChannel) Push(ctx context.Context, msg *message.Msg) error {
	if m.push != nil {
		return m.push(ctx, msg)
	}
	return nil
}
func (m *mockChannel) Reply(id uint64, data []byte, err error) error { return nil }
func (m *mockChannel) Prev(p ...bucket.IChannel) bucket.IChannel     { return nil }
func (m *mockChannel) Next(n ...bucket.IChannel) bucket.IChannel     { return nil }
func (m *mockChannel) Room(r ...*bucket.Room) *bucket.Room           { return nil }
func (m *mockChannel) UserId(u ...int64) int64                       { return m.id }
func (m *mockChannel) Token(t ...string) string                      { return "" }
func (m *mockChannel) Close() error                                  { return nil }

// ---------------------------------------------------------------------------
// Room 成员管理
// ---------------------------------------------------------------------------

func newRoomWithMembers(n int) (*bucket.Room, []*mockChannel) {
	r := bucket.NewRoom(1)
	chs := make([]*mockChannel, n)
	for i := 0; i < n; i++ {
		chs[i] = &mockChannel{id: int64(i)}
		_ = r.Join(chs[i])
	}
	return r, chs
}

func BenchmarkRoomJoin1000(b *testing.B) {
	r := bucket.NewRoom(1)
	chs := make([]*mockChannel, 1000)
	for i := range chs {
		chs[i] = &mockChannel{id: int64(i)}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Join(chs[i%1000])
	}
}

func BenchmarkRoomLeave1000(b *testing.B) {
	r, chs := newRoomWithMembers(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Leave(chs[i%1000])
	}
}

func BenchmarkRoomEach100(b *testing.B) {
	r, _ := newRoomWithMembers(100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Each(func(ch bucket.IChannel) {})
	}
}

func BenchmarkRoomEach1000(b *testing.B) {
	r, _ := newRoomWithMembers(1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Each(func(ch bucket.IChannel) {})
	}
}

func BenchmarkRoomEach10000(b *testing.B) {
	r, _ := newRoomWithMembers(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Each(func(ch bucket.IChannel) {})
	}
}

func BenchmarkRoomBroadcast100(b *testing.B) {
	r, _ := newRoomWithMembers(100)
	msg := message.NewTextMessage([]byte("hello world"))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Broadcast(context.Background(), msg)
	}
}

// ---------------------------------------------------------------------------
// frame 分片编解码
// ---------------------------------------------------------------------------

func BenchmarkFrameEncode(b *testing.B) {
	ds := &frame.DataSlice{
		P: frame.BinaryMessage,
		N: "m",
		T: 1,
		I: 0,
		S: 128,
		D: make([]byte, 128),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = frame.Encode(ds)
	}
}

func BenchmarkFrameDecode(b *testing.B) {
	ds := &frame.DataSlice{
		P: frame.BinaryMessage,
		N: "m",
		T: 1,
		I: 0,
		S: 128,
		D: make([]byte, 128),
	}
	buf := frame.Encode(ds)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = frame.Decode(buf)
	}
}

// ---------------------------------------------------------------------------
// AG 参数编解码
// ---------------------------------------------------------------------------

func BenchmarkAgEncodeInt(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ag.Encode(42)
	}
}

func BenchmarkAgEncodeString(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ag.Encode("hello world, 你好世界")
	}
}

func BenchmarkAgEncodeStruct(b *testing.B) {
	type user struct {
		Id   int64  `json:"id"`
		Name string `json:"name"`
	}
	u := user{Id: 1, Name: "sloth"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ag.Encode(u)
	}
}

func BenchmarkAgDecode(b *testing.B) {
	buf, _ := ag.Encode("hello world")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ag.Decoder(buf)
	}
}

// ---------------------------------------------------------------------------
// Header TLV 编解码
// ---------------------------------------------------------------------------

func BenchmarkHeaderBytes(b *testing.B) {
	h := message.Header{"uid": "10086", "token": "abc123", "seq": "1"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = h.Bytes()
	}
}

func BenchmarkHeaderClone(b *testing.B) {
	h := message.Header{"uid": "10086", "token": "abc123", "seq": "1"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.Clone()
	}
}
