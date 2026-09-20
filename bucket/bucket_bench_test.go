package bucket

import (
	"context"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/w6xian/sloth/v3/message"
)

// discardLogs 关闭广播队列满时的限流日志：
// 基准会持续打满队列，日志（全局互斥锁 + IO）会淹没 benchmark 输出并干扰计时。
func discardLogs(b *testing.B) {
	b.Helper()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(os.Stderr) })
}

// ---------------------------------------------------------------------------
// 补充测试
// ---------------------------------------------------------------------------

// slowCloseChannel Close 会一直阻塞（模拟真实连接 Close 时正卡在 SendData 上），
// 用于验证 Put 是在**释放桶锁之后**才关闭被抢占的旧连接。
type slowCloseChannel struct {
	*mockChannel
	closeEntered chan struct{}
	release      chan struct{}
}

func (c *slowCloseChannel) Close() error {
	select {
	case c.closeEntered <- struct{}{}:
	default:
	}
	<-c.release
	return nil
}

// TestBucketPutClosesOldConnOutsideLock 回归测试：
// 被抢占的旧连接 Close 阻塞时，整桶不能被拖住（原实现在持写锁状态下关闭旧连接，
// 高并发重连会把 Put/Channel/DeleteChannel 全部阻塞到连接超时）。
func TestBucketPutClosesOldConnOutsideLock(t *testing.T) {
	b := NewBucket(WithRoutineAmount(1))
	defer b.Close()

	old := &slowCloseChannel{
		mockChannel:  newMockChannels(1)[0],
		closeEntered: make(chan struct{}, 1),
		release:      make(chan struct{}),
	}
	if err := b.Put(1, 1, "t", old); err != nil {
		t.Fatal(err)
	}

	newCh := newMockChannels(1)[0]
	putDone := make(chan error, 1)
	go func() { putDone <- b.Put(1, 1, "t", newCh) }()

	select {
	case <-old.closeEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("old channel Close was not called")
	}

	// 旧连接 Close 仍阻塞中，桶必须依然可服务
	done := make(chan struct{})
	go func() {
		_ = b.Channel(1)
		_ = b.Room(1)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("bucket blocked while closing old conn: Close must run outside the bucket lock")
	}

	close(old.release)
	select {
	case err := <-putDone:
		if err != nil {
			t.Fatalf("Put err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Put did not return after old conn released")
	}
}

// TestBucketBroadcastRoomQueueFull 队列满时必须非阻塞返回 false（尽力而为语义），
// 调用方不应被下游消费速度拖住；队列腾出后投递恢复。
func TestBucketBroadcastRoomQueueFull(t *testing.T) {
	b := NewBucket(WithRoutineAmount(1), WithRoutineSize(1))
	defer b.Close()

	ch := newMockChannels(1)[0]
	if err := b.Put(1, 1, "t", ch); err != nil {
		t.Fatal(err)
	}

	// 持有写锁 → worker 在 b.Room() 处阻塞 → 队列(容量1)必然被填满
	b.cLock.Lock()
	ok1 := b.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: 1, Msg: message.NewTextMessage([]byte("first"))})
	ok2 := b.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: 1, Msg: message.NewTextMessage([]byte("second"))})
	b.cLock.Unlock()

	if !ok1 {
		t.Fatal("first enqueue should succeed")
	}
	if ok2 {
		t.Fatal("queue full should return false instead of blocking")
	}

	// 放开锁后 worker 消费完，队列腾出，投递恢复
	deadline := time.Now().Add(3 * time.Second)
	requeued := false
	for time.Now().Before(deadline) {
		if b.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: 1, Msg: message.NewTextMessage([]byte("third"))}) {
			requeued = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !requeued {
		t.Fatal("enqueue should recover after worker drains the queue")
	}
	for time.Now().Before(deadline) {
		if ch.push.Load() >= 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("messages not delivered, push count = %d, want >= 2", ch.push.Load())
}

// TestBucketBroadcastAllManyRooms 万级房间广播：全部成功投递、无 panic、无死锁。
func TestBucketBroadcastAllManyRooms(t *testing.T) {
	const rooms = 10000
	b := NewBucket(WithRoutineAmount(32), WithRoutineSize(1024), WithRoomSize(rooms))
	defer b.Close()

	chs := newMockChannels(rooms)
	for i, ch := range chs {
		if err := b.Put(int64(i+100000), int64(i+1), "t", ch); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	if dropped := b.BroadcastAll(context.Background(), message.NewTextMessage([]byte("broadcast"))); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		done := true
		for _, ch := range chs {
			if ch.push.Load() == 0 {
				done = false
				break
			}
		}
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("broadcast to 10000 rooms not delivered in time")
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkBucketPut(b *testing.B) {
	bk := NewBucket(WithRoutineAmount(2), WithChannelSize(1024))
	defer bk.Close()
	chs := newMockChannels(1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := bk.Put(int64(i%1024+10000), int64(i%16+1), "t", chs[i%1024]); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBucketPutParallel(b *testing.B) {
	bk := NewBucket(WithRoutineAmount(4), WithChannelSize(4096))
	defer bk.Close()
	chs := newMockChannels(4096)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % 4096
			if err := bk.Put(int64(idx+10000), int64(idx%32+1), "t", chs[idx]); err != nil {
				b.Error(err)
				return
			}
			i++
		}
	})
}

func BenchmarkBucketChannel(b *testing.B) {
	bk := NewBucket(WithRoutineAmount(2))
	defer bk.Close()
	chs := newMockChannels(1024)
	for i, ch := range chs {
		if err := bk.Put(int64(i+10000), 1, "t", ch); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bk.Channel(int64(i%1024 + 10000))
	}
}

func BenchmarkBucketDeleteChannel(b *testing.B) {
	bk := NewBucket(WithRoutineAmount(2))
	defer bk.Close()
	chs := newMockChannels(1024)
	for i, ch := range chs {
		if err := bk.Put(int64(i+10000), int64(i+1), "t", ch); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bk.DeleteChannel(chs[i%1024])
	}
}

// 全服广播：BroadcastAll（读锁内直接遍历 + 请求对象池化）
func BenchmarkBucketBroadcastAll(b *testing.B) {
	discardLogs(b)
	for _, rooms := range []int{100, 1000, 10000} {
		bk := NewBucket(WithRoutineAmount(32), WithRoutineSize(1024), WithRoomSize(rooms))
		chs := newMockChannels(rooms)
		for i, ch := range chs {
			if err := bk.Put(int64(i+100000), int64(i+1), "t", ch); err != nil {
				b.Fatal(err)
			}
		}
		msg := message.NewTextMessage([]byte("hello broadcast"))
		b.Run("rooms"+itoaBucket(rooms), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = bk.BroadcastAll(context.Background(), msg)
			}
		})
		bk.Close()
	}
}

// BenchmarkBucketBroadcastStrategies 对照两种广播写法：
//  1. 读锁内直接遍历 map（本函数内 nosnapshot 分支，即 BroadcastAll 的旧写法）
//  2. RangeRooms 全量快照后锁外投递（当前 BroadcastAll 采用的写法）
//
// 两者都直接新建请求对象：池化已被实测证伪——32 worker 并发消费时 pool 的
// 跨 P 窃取/加锁开销高于小对象分配（10000 房间：池化 3.06ms/op vs 新建 1.39ms/op）。
// 用**空房间**（无成员）构建桶：worker 消费几乎零成本，队列不会被打满，
// 因此计时反映的是"投递路径"本身的开销，而不是消息送达的成本。
func BenchmarkBucketBroadcastStrategies(b *testing.B) {
	discardLogs(b)
	for _, rooms := range []int{1000, 10000} {
		for _, s := range []struct {
			name     string
			snapshot bool
		}{
			{"nosnapshot", false},
			{"snapshot", true},
		} {
			bk := NewBucket(WithRoutineAmount(32), WithRoutineSize(1024), WithRoomSize(rooms))
			for i := 0; i < rooms; i++ {
				bk.rooms[int64(i+1)] = NewRoom(int64(i + 1))
			}
			msg := message.NewTextMessage([]byte("hello broadcast"))
			b.Run("rooms"+itoaBucket(rooms)+"/"+s.name, func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if s.snapshot {
						bk.RangeRooms(func(room *Room) bool {
							_ = bk.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: room.Id, Msg: msg})
							return true
						})
						continue
					}
					bk.cLock.RLock()
					for rid, room := range bk.rooms {
						if room.IsDrop() {
							continue
						}
						_ = bk.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: rid, Msg: msg})
					}
					bk.cLock.RUnlock()
				}
			})
			bk.Close()
		}
	}
}

func itoaBucket(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
