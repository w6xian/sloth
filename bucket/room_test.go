package bucket

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/w6xian/sloth/v3/message"
)

// mockChannel 最小化实现 IChannel，供 Room 测试使用。
type mockChannel struct {
	id     int64
	user   int64
	room   *Room
	push   atomic.Int64 // Push 调用次数
	closed atomic.Bool  // Close 是否被调用
}

func (m *mockChannel) Call(ctx context.Context, header message.Header, mtd string, args ...[]byte) ([]byte, error) {
	return nil, nil
}
func (m *mockChannel) SendData(ctx context.Context, msgId uint64, payload []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockChannel) Receive(tx context.Context, payload []byte) error { return nil }
func (m *mockChannel) Push(ctx context.Context, msg *message.Msg) error {
	m.push.Add(1)
	return nil
}
func (m *mockChannel) Send(ctx context.Context, id uint64, data []byte, err error) error { return nil }
func (m *mockChannel) Prev(p ...IChannel) IChannel                                       { return nil }
func (m *mockChannel) Next(n ...IChannel) IChannel                                       { return nil }
func (m *mockChannel) Room(r ...*Room) *Room {
	if len(r) > 0 {
		m.room = r[0]
	}
	return m.room
}
func (m *mockChannel) UserId(u ...int64) int64 {
	if len(u) > 0 {
		m.user = u[0]
	}
	return m.user
}
func (m *mockChannel) Token(t ...string) string { return "" }
func (m *mockChannel) Close() error {
	m.closed.Store(true)
	return nil
}

func newMockChannels(n int) []*mockChannel {
	chs := make([]*mockChannel, n)
	for i := 0; i < n; i++ {
		chs[i] = &mockChannel{id: int64(i), user: int64(i)}
	}
	return chs
}

// ---------------------------------------------------------------------------
// 基本操作
// ---------------------------------------------------------------------------

func TestRoomJoinLeave(t *testing.T) {
	r := NewRoom(1)
	ch := newMockChannels(1)[0]

	if err := r.Join(ch); err != nil {
		t.Fatalf("Join error: %v", err)
	}
	if !r.Contains(ch) {
		t.Fatal("Contains(ch) should be true after Join")
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d, want 1", r.Len())
	}

	// 重复 Join 幂等
	if err := r.Join(ch); err != nil {
		t.Fatalf("duplicate Join error: %v", err)
	}
	if r.Len() != 1 {
		t.Fatalf("Len = %d after duplicate Join, want 1", r.Len())
	}

	// Leave：最后一名成员退出后，普通房间应 Drop（空房解散）
	if drop := r.Leave(ch); !drop {
		t.Fatal("Leave should drop room 1 when last member leaves")
	}
	if r.Contains(ch) {
		t.Fatal("Contains(ch) should be false after Leave")
	}
	if r.Len() != 0 {
		t.Fatalf("Len = %d, want 0", r.Len())
	}
	// Leave 不在房间的 channel 不应 panic，且保持 Drop 状态
	if !r.Leave(ch) {
		t.Fatal("Leave of non-member should still report dropped state")
	}
}

func TestRoomDropWhenEmpty(t *testing.T) {
	// 非 Plaza 房间空后 Drop
	r := NewRoom(1)
	ch := newMockChannels(1)[0]
	_ = r.Join(ch)
	if drop := r.Leave(ch); !drop {
		t.Fatal("Leave should mark room dropped when empty")
	}
	if !r.IsDrop() {
		t.Fatal("IsDrop should be true")
	}

	// Drop 后 Join 报错
	if err := r.Join(ch); err == nil {
		t.Fatal("Join on dropped room should return error")
	}

	// Plaza 房间空后不 Drop
	p := NewRoom(Plaza)
	_ = p.Join(ch)
	if drop := p.Leave(ch); drop {
		t.Fatal("Plaza room should never drop")
	}
	if p.IsDrop() {
		t.Fatal("Plaza IsDrop should be false")
	}
	if p.Len() != 0 {
		t.Fatalf("Plaza Len = %d, want 0", p.Len())
	}
}

func TestRoomJoinDropped(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(2)
	_ = r.Join(chs[0])
	r.Leave(chs[0]) // 现在 r 已 Drop

	if err := r.Join(chs[1]); err == nil {
		t.Fatal("Join on dropped room should return error")
	}
}

// ---------------------------------------------------------------------------
// 多房间：同一个 channel 可加入多个 Room
// ---------------------------------------------------------------------------

func TestChannelInMultipleRooms(t *testing.T) {
	r1 := NewRoom(1)
	r2 := NewRoom(2)
	r3 := NewRoom(3)
	ch := newMockChannels(1)[0]

	if err := r1.Join(ch); err != nil {
		t.Fatal(err)
	}
	if err := r2.Join(ch); err != nil {
		t.Fatal(err)
	}
	if err := r3.Join(ch); err != nil {
		t.Fatal(err)
	}

	for _, r := range []*Room{r1, r2, r3} {
		if !r.Contains(ch) {
			t.Fatalf("room %d should contain ch", r.Id)
		}
	}

	// 退出 r1 不影响 r2/r3
	r1.Leave(ch)
	if r1.Contains(ch) {
		t.Fatal("r1 should not contain ch after Leave")
	}
	if !r2.Contains(ch) || !r3.Contains(ch) {
		t.Fatal("r2/r3 should still contain ch")
	}
}

// ---------------------------------------------------------------------------
// 遍历
// ---------------------------------------------------------------------------

func TestRoomEach(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(5)
	for _, ch := range chs {
		_ = r.Join(ch)
	}

	seen := make(map[int64]bool)
	r.Each(func(ch IChannel) {
		seen[ch.UserId()] = true
	})
	if len(seen) != 5 {
		t.Fatalf("Each visited %d channels, want 5", len(seen))
	}
}

func TestRoomRangeEarlyStop(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(10)
	for _, ch := range chs {
		_ = r.Join(ch)
	}

	count := 0
	r.Range(func(ch IChannel) bool {
		count++
		return count < 3 // 第 3 次后提前终止
	})
	if count != 3 {
		t.Fatalf("Range visited %d, want 3 (early stop)", count)
	}
}

// 遍历回调中 Join/Leave 不应死锁（快照语义）。
func TestRoomEachWithMutationInCallback(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(5)
	for _, ch := range chs {
		_ = r.Join(ch)
	}
	extra := newMockChannels(1)[0]

	done := make(chan struct{})
	go func() {
		r.Each(func(ch IChannel) {
			_ = r.Join(extra) // 回调中写房间
			r.Leave(ch)       // 回调中删除当前成员
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Each with mutation in callback deadlocked")
	}
}

func TestRoomEachBatch(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(7)
	for _, ch := range chs {
		_ = r.Join(ch)
	}

	var total int
	var maxBatch int
	r.EachBatch(3, func(batch []IChannel) {
		total += len(batch)
		if len(batch) > maxBatch {
			maxBatch = len(batch)
		}
	})
	if total != 7 {
		t.Fatalf("EachBatch visited %d, want 7", total)
	}
	if maxBatch > 3 {
		t.Fatalf("max batch = %d, want <= 3", maxBatch)
	}
}

// ---------------------------------------------------------------------------
// 广播
// ---------------------------------------------------------------------------

func TestRoomBroadcast(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(5)
	for _, ch := range chs {
		_ = r.Join(ch)
	}

	msg := message.NewTextMessage([]byte("hi"))
	r.Broadcast(context.Background(), msg)

	for i, ch := range chs {
		if got := ch.push.Load(); got != 1 {
			t.Fatalf("channel %d push count = %d, want 1", i, got)
		}
	}
}

// ---------------------------------------------------------------------------
// 并发安全（-race 下验证）
// ---------------------------------------------------------------------------

func TestRoomConcurrentJoinLeave(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(64)

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch := chs[i]
			for j := 0; j < 200; j++ {
				_ = r.Join(ch)
				r.Leave(ch)
				r.Contains(ch)
				r.Len()
				r.IsDrop()
			}
		}(i)
	}
	wg.Wait()
	if r.Len() != 0 {
		t.Fatalf("Len = %d after concurrent ops, want 0", r.Len())
	}
}

func TestRoomConcurrentEachAndJoin(t *testing.T) {
	r := NewRoom(1)
	chs := newMockChannels(20)
	for _, ch := range chs {
		_ = r.Join(ch)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // 遍历 goroutine
		defer wg.Done()
		for j := 0; j < 500; j++ {
			r.Each(func(ch IChannel) {})
		}
	}()
	go func() { // 增删 goroutine
		defer wg.Done()
		extra := newMockChannels(10)
		for j := 0; j < 500; j++ {
			_ = r.Join(extra[j%10])
			r.Leave(extra[j%10])
		}
	}()
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Bucket 级
// ---------------------------------------------------------------------------

// TestBucketBroadcastAll 验证 BroadcastAll 投递到所有房间的所有成员（异步，需轮询等待 worker 消费）。
// 已解散(Drop)房间应被跳过且不计入 dropped。
func TestBucketBroadcastAll(t *testing.T) {
	b := NewBucket(WithRoutineAmount(4), WithRoutineSize(8))
	defer b.Close()

	// 3 个房间 × 3 个成员
	chs := newMockChannels(9)
	for i, ch := range chs {
		roomID := int64(i/3 + 1)
		if err := b.Put(int64(i+100), roomID, "t", ch); err != nil {
			t.Fatal(err)
		}
	}
	// 手动塞入一个已解散房间，验证被跳过且不影响 dropped
	b.rooms[99] = &Room{Id: 99, Drop: true}

	msg := message.NewTextMessage([]byte("all"))
	if dropped := b.BroadcastAll(context.Background(), msg); dropped != 0 {
		t.Fatalf("dropped = %d, want 0", dropped)
	}

	// 异步消费，轮询等待全部到达
	deadline := time.Now().Add(2 * time.Second)
	for {
		done := true
		for _, ch := range chs {
			if ch.push.Load() != 1 {
				done = false
				break
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("BroadcastAll 异步投递超时未被 worker 消费")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestBucketDeleteChannelConcurrent 回归验证 DeleteChannel 在并发下不 panic：
// 旧实现在 RLock 下执行 map 写操作，并发调用会触发 fatal error: concurrent map writes。
func TestBucketDeleteChannelConcurrent(t *testing.T) {
	b := NewBucket(WithRoutineAmount(4))
	defer b.Close()

	chs := newMockChannels(32)
	// userId 取 i+1000，避免与 mockChannel 初始 user(=i) 重叠：
	// Put 内部用 ch.UserId() 查重，重叠会让新连接误伤已存连接（业务无关）。
	for i, ch := range chs {
		if err := b.Put(int64(i+1000), 1, "t", ch); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, ch := range chs {
		wg.Add(1)
		go func(ch *mockChannel) {
			defer wg.Done()
			b.DeleteChannel(ch)
		}(ch)
	}
	wg.Wait()

	// 所有 channel 删除后，房间 1 应解散并从 map 回收
	b.cLock.RLock()
	_, ok := b.rooms[1]
	b.cLock.RUnlock()
	if ok {
		t.Fatal("room 1 should be removed after all channels deleted")
	}
	if len(b.chs) != 0 {
		t.Fatalf("chs len = %d, want 0", len(b.chs))
	}
}

// TestBucketClose 验证 Close 能停止全部 worker 且不阻塞。
func TestBucketClose(t *testing.T) {
	b := NewBucket(WithRoutineAmount(2))
	done := make(chan struct{})
	go func() {
		b.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Bucket.Close deadlocked")
	}
}

// TestBucketRangeChannels 验证 RangeChannels 语义：
// 1) 覆盖全部在线连接（含未入任何房间的连接）
// 2) 同一连接同时在多个房间也只出现一次（chs 唯一映射去重）
// 3) fn 返回 false 提前终止
func TestBucketRangeChannels(t *testing.T) {
	b := NewBucket(WithRoutineAmount(2))
	defer b.Close()

	chs := newMockChannels(9)
	want := make(map[int64]*mockChannel) // userId -> ch
	for i, ch := range chs {
		uid := int64(i + 200) // 与 mock 初始 user(=i) 错开，避免 Put 查重误伤
		want[uid] = ch
	}
	// 0..5 入房间 1；6..7 入房间 2；8 不入任何房间
	for i, ch := range chs[:6] {
		if err := b.Put(int64(i+200), 1, "t", ch); err != nil {
			t.Fatal(err)
		}
	}
	for i, ch := range chs[6:8] {
		if err := b.Put(int64(i+206), 2, "t", ch); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Put(208, NoRoom, "t", chs[8]); err != nil {
		t.Fatal(err)
	}
	// chs[6] 额外加入房间 3：同一连接同时属于房间 2 与 3
	room3 := NewRoom(3)
	b.rooms[3] = room3
	_ = room3.Join(chs[6])

	seen := make(map[int64]int)
	b.RangeChannels(func(ch IChannel) bool {
		seen[ch.UserId()]++
		return true
	})
	if len(seen) != len(want) {
		t.Fatalf("RangeChannels visited %d channels, want %d", len(seen), len(want))
	}
	for uid, n := range seen {
		if n != 1 {
			t.Fatalf("channel %d visited %d times, want 1 (多房间去重失败)", uid, n)
		}
		if _, ok := want[uid]; !ok {
			t.Fatalf("unexpected channel %d", uid)
		}
	}

	// 提前终止
	cnt := 0
	b.RangeChannels(func(ch IChannel) bool {
		cnt++
		return cnt < 3
	})
	if cnt != 3 {
		t.Fatalf("RangeChannels early stop got %d, want 3", cnt)
	}
}

// TestBucketReconnectReplace 验证 F5 强刷新重连语义：
// 同 userId 不同连接对象重新 Put，旧连接必须被回收（退出房间 + 关闭），新连接接管注册表。
func TestBucketReconnectReplace(t *testing.T) {
	b := NewBucket(WithRoutineAmount(2))
	defer b.Close()

	oldCh := newMockChannels(1)[0]
	if err := b.Put(1, 10, "t1", oldCh); err != nil {
		t.Fatal(err)
	}

	// 模拟 F5 强刷新：同 userId 新连接对象（UserId 尚未设置，模拟常见重连时序）
	newCh := &mockChannel{id: 100, user: 0}
	if err := b.Put(1, 10, "t2", newCh); err != nil {
		t.Fatal(err)
	}

	// 新连接接管注册表
	b.cLock.RLock()
	cur := b.chs[1]
	b.cLock.RUnlock()
	if cur != newCh {
		t.Fatalf("chs[1] = %v, want new channel", cur)
	}
	// 旧连接被关闭并退出房间
	if !oldCh.closed.Load() {
		t.Fatal("old channel should be closed after reconnect")
	}
	r := b.Room(10)
	if r == nil || !r.Contains(newCh) || r.Contains(oldCh) {
		t.Fatalf("room 10 should contain only the new channel")
	}
	// 同一对象重复 login 仍幂等：只更新 token，不重复注册、不关闭自己
	if err := b.Put(1, 10, "t3", newCh); err != nil {
		t.Fatal(err)
	}
	if newCh.closed.Load() {
		t.Fatal("same-channel re-login must not close itself")
	}
}

// TestBucketDeleteChannelOnlySelf 验证 DeleteChannel 只删除传入的自身：
// 断开的旧连接晚于新连接执行清理时（F5 重连竞态），不得误删已接管的新连接。
func TestBucketDeleteChannelOnlySelf(t *testing.T) {
	b := NewBucket(WithRoutineAmount(2))
	defer b.Close()

	oldCh := newMockChannels(1)[0]
	if err := b.Put(1, 10, "t1", oldCh); err != nil {
		t.Fatal(err)
	}
	// 新连接已注册（覆盖注册表）
	newCh := &mockChannel{id: 100, user: 0}
	if err := b.Put(1, 10, "t2", newCh); err != nil {
		t.Fatal(err)
	}
	// 旧连接的清理晚到：不得删除新连接
	b.DeleteChannel(oldCh)
	b.cLock.RLock()
	cur, ok := b.chs[1]
	b.cLock.RUnlock()
	if !ok || cur != newCh {
		t.Fatal("DeleteChannel of a stale connection must not remove the new one")
	}
	if r := b.Room(10); r == nil || !r.Contains(newCh) {
		t.Fatal("new channel should still be in room 10")
	}
}

// TestBucketRangeChannelsConcurrentPutDelete 验证快照遍历与并发增删互不阻塞、无数据竞争。
func TestBucketRangeChannelsConcurrentPutDelete(t *testing.T) {
	b := NewBucket(WithRoutineAmount(2))
	defer b.Close()

	chs := newMockChannels(10)
	for i, ch := range chs {
		if err := b.Put(int64(i+300), 1, "t", ch); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // 遍历
		defer wg.Done()
		for j := 0; j < 200; j++ {
			b.RangeChannels(func(ch IChannel) bool { return true })
		}
	}()
	go func() { // 增删
		defer wg.Done()
		extra := newMockChannels(5)
		for j := 0; j < 200; j++ {
			_ = b.Put(int64(j+400), 1, "t", extra[j%5])
			b.DeleteChannel(extra[j%5])
		}
	}()
	wg.Wait()
}
