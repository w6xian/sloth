package sloth

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/internal/logger"
	"github.com/w6xian/sloth/v4/nrpc/wsocket"
	"github.com/w6xian/sloth/v4/types"
)

// TestRoomAcrossBucketShards 同一房间的成员分布在不同 bucket 分片时，
// 房间操作必须覆盖全部分片。
//
// 背景：连接按 userId 分桶，而同一房间的成员 userId 通常落在不同分片，
// 每个分片各持有一个同 roomId 的 Room。只取 Room(roomId) 的第一个分片会漏掉
// 其它分片的成员——表现为"房间广播有人收不到"。Rooms() 就是为修这个问题加的。
func TestRoomAcrossBucketShards(t *testing.T) {
	logger.SetOutput(io.Discard)
	defer logger.SetOutput(os.Stderr)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	svr := ServerConn(DefaultServer())
	if err := svr.Listen(ctx, "ws", "127.0.0.1:0"); err != nil {
		t.Fatalf("server listen: %v", err)
	}
	addr := svr.listeners[0].Listener.Addr().String()
	go svr.Serve()
	defer svr.Close()
	waitServerReady(t, ctx, addr)

	// 传输实例由 ProtocolFactory 创建并挂在 listener 上：
	// 此前它藏在 Connect.wsServer 字段里（而且 ws 自己也绕过了工厂）。
	ws, ok := svr.listeners[0].Server.(*wsocket.WsServer)
	if !ok || ws == nil {
		t.Fatalf("传输实例 = %T, want *wsocket.WsServer（ProtocolFactory 没被使用？）", svr.listeners[0].Server)
	}
	if len(ws.Buckets) < 2 {
		t.Skipf("需要至少 2 个 bucket 分片，当前 %d", len(ws.Buckets))
	}

	// 同一房间(100)的两条连接分别落在分片 0 与分片 1
	ch1 := wsocket.NewWsChannelServer(nil)
	ch2 := wsocket.NewWsChannelServer(nil)
	if err := ws.Buckets[0].Put(1, 100, "t1", ch1); err != nil {
		t.Fatalf("put ch1: %v", err)
	}
	if err := ws.Buckets[1].Put(2, 100, "t2", ch2); err != nil {
		t.Fatalf("put ch2: %v", err)
	}

	// Room 只返回一个分片（保持 types.IServer 的单值语义）
	if r := ws.Room(100); r == nil {
		t.Fatal("Room(100) should not be nil")
	}
	// Rooms 必须覆盖两个分片，成员合计 2
	rooms := ws.Rooms(100)
	if len(rooms) != 2 {
		t.Fatalf("Rooms(100) = %d shards, want 2", len(rooms))
	}
	total := 0
	for _, r := range rooms {
		r.Range(func(ch bucket.IChannel) bool {
			total++
			return true
		})
	}
	if total != 2 {
		t.Fatalf("房间成员合计 = %d, want 2（跨分片成员被漏掉）", total)
	}
	// 根包 helper 走的是 Rooms 分支（WsServer 实现了该方法）
	if got := len(serverRooms(ws, 100)); got != 2 {
		t.Fatalf("serverRooms(100) = %d shards, want 2", got)
	}
}

// TestServerRoomsFallsBackToSingle 不支持列举分片的实现退化为单个结果。
func TestServerRoomsFallsBackToSingle(t *testing.T) {
	stub := &stubRoomServer{room: bucket.NewRoom(42)}
	if got := serverRooms(stub, 42); len(got) != 1 {
		t.Fatalf("serverRooms = %d, want 1（退化分支）", len(got))
	}
	if serverRooms(stub, 43) != nil {
		t.Fatal("不存在的房间应返回 nil")
	}
}

// stubRoomServer 只实现 serverRooms 需要的 Room 方法，用于验证退化分支。
type stubRoomServer struct {
	types.IServer
	room *bucket.Room
}

func (s *stubRoomServer) Room(roomId int64) *bucket.Room {
	if s.room != nil && s.room.Id == roomId {
		return s.room
	}
	return nil
}
