/*
 * 用于存放用户连接的桶，每个桶有多个房间，每个房间有多个连接，每个连接有一个用户
 */
package bucket

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"github.com/w6xian/sloth/v3/message"
)

type Bucket struct {
	cLock sync.RWMutex       // protect the channels for chs
	chs   map[int64]IChannel // map sub key to a channel

	rooms       map[int64]*Room // bucket room channels
	routines    []chan *message.PushRoomMsgRequest
	routinesNum atomic.Uint64
	dropped     atomic.Uint64 // 广播投递累计丢弃数（用于日志限流）

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	ChannelSize   int
	RoomSize      int
	RoutineAmount uint64
	RoutineSize   int
}

func NewBucket(opts ...BucketOption) (b *Bucket) {
	b = new(Bucket)
	b.ChannelSize = 1024
	b.RoomSize = 1024
	b.RoutineAmount = 32
	b.RoutineSize = 20
	for _, opt := range opts {
		opt(b)
	}

	b.chs = make(map[int64]IChannel, b.ChannelSize)
	b.routines = make([]chan *message.PushRoomMsgRequest, b.RoutineAmount)
	b.rooms = make(map[int64]*Room, b.RoomSize)
	b.ctx, b.cancel = context.WithCancel(context.Background())
	b.wg.Add(int(b.RoutineAmount))
	for i := uint64(0); i < b.RoutineAmount; i++ {
		c := make(chan *message.PushRoomMsgRequest, b.RoutineSize)
		b.routines[i] = c
		go b.PushRoom(b.ctx, c)
	}
	return
}

// Close 停止所有 worker goroutine 并等待其退出。
// 调用后 Bucket 不再可用（禁止再投递广播）。
func (b *Bucket) Close() {
	b.cancel()
	b.wg.Wait()
}

func (b *Bucket) PushRoom(ctx context.Context, ch chan *message.PushRoomMsgRequest) {
	defer b.wg.Done()
	for {
		select {
		case arg := <-ch:
			if room := b.Room(arg.RoomId); room != nil {
				room.Broadcast(ctx, arg.Msg)
			}
		case <-ctx.Done():
			return
		}
	}
}

// RangeRooms 安全遍历桶内所有房间：读锁下快照后锁外回调，fn 返回 false 提前终止。
// 相比直接遍历裸 map，可避免遍历期间其他 goroutine 增删房间导致的并发读写 panic。
// 注意：每次调用都会全量拷贝房间快照，若回调仅为广播投递（非阻塞、不碰桶锁），
// 请优先使用 BroadcastAll，它在读锁内直接遍历，零额外分配。
func (b *Bucket) RangeRooms(fn func(room *Room) bool) {
	b.cLock.RLock()
	rooms := make([]*Room, 0, len(b.rooms))
	for _, room := range b.rooms {
		rooms = append(rooms, room)
	}
	b.cLock.RUnlock()
	for _, room := range rooms {
		if !fn(room) {
			return
		}
	}
}

// BroadcastAll 向桶内所有房间异步广播一条消息，返回因 worker 队列满而丢弃的房间数。
// 广播热路径专用：在读锁内直接遍历房间 map 并做非阻塞投递。
// 投递（BroadcastRoom）只涉及原子计数与无阻塞 channel 写，不触碰桶锁，锁内执行安全，
// 因此无需像 RangeRooms 那样先全量拷贝快照，省掉每次广播的一次整表分配。
// 已解散(Drop)的房间跳过；若投递失败（队列满）调用方可根据返回值决定是否降级。
func (b *Bucket) BroadcastAll(ctx context.Context, msg *message.Msg) (dropped int) {
	b.cLock.RLock()
	defer b.cLock.RUnlock()
	for rid, room := range b.rooms {
		if room.IsDrop() {
			continue
		}
		if !b.BroadcastRoom(&message.PushRoomMsgRequest{RoomId: rid, Msg: msg}) {
			dropped++
		}
	}
	return
}

// RangeChannels 安全遍历桶内所有在线连接：读锁下快照后锁外回调，fn 返回 false 提前终止。
// b.chs 是 user->channel 的唯一映射：同一连接即使同时在多个房间，也只会被遍历一次，
// 天然去重，适合"对每个连接做一次操作"的场景（如 CallBucket 全服 RPC）。
// 与 RangeRooms 的差异：覆盖全部在线连接（含未入任何房间的连接），而非仅房间成员。
func (b *Bucket) RangeChannels(fn func(ch IChannel) bool) {
	b.cLock.RLock()
	chs := make([]IChannel, 0, len(b.chs))
	for _, ch := range b.chs {
		chs = append(chs, ch)
	}
	b.cLock.RUnlock()
	for _, ch := range chs {
		if !fn(ch) {
			return
		}
	}
}

func (b *Bucket) Room(rid int64) (room *Room) {
	b.cLock.RLock()
	room = b.rooms[rid]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) Put(userId int64, roomId int64, token string, ch IChannel) (err error) {
	var (
		room *Room
		ok   bool
	)
	b.cLock.Lock()
	defer b.cLock.Unlock()

	if ch0, ch_ok := b.chs[ch.UserId()]; ch_ok {
		ch0Room := ch0.Room()
		// 只是更新 token（userId 相同、roomId 相同、两者要么都没房间要么房间ID一致）
		if ch0.UserId() == userId &&
			((ch0Room == nil && roomId == NoRoom) ||
				(ch0Room != nil && ch0Room.Id == roomId)) {
			ch0.Token(token)
			return
		}

		// 原来有房间，且不是当前房间，先退出房间
		if ch0Room != nil && ch0Room.Id != roomId {
			ch0Room.Leave(ch0)
		}
		// userId 改变，需要更新桶中的连接
		if ch0.UserId() != userId && ch0.UserId() > 0 {
			// 关闭后，删除桶中的连接
			ch0.Close()
			delete(b.chs, ch0.UserId())
		}
	}
	// 原来有房间，先退出房间
	if curRoom := ch.Room(); curRoom != nil {
		if curRoom.Id != roomId {
			curRoom.Leave(ch)
		}
	}
	if roomId != NoRoom {
		if room, ok = b.rooms[roomId]; !ok {
			room = NewRoom(roomId)
			b.rooms[roomId] = room
		}
		if room.Drop {
			room = NewRoom(roomId)
			b.rooms[roomId] = room
		}
		ch.Room(room)
	}
	ch.UserId(userId)
	ch.Token(token)
	b.chs[userId] = ch
	if room != nil {
		err = room.Join(ch)
	}
	return
}

// 通出房间
func (b *Bucket) Quit(ch IChannel) (err error) {
	var (
		room *Room
		ok   bool
	)
	b.cLock.Lock()
	defer b.cLock.Unlock()
	if ch.Room() == nil {
		return
	}
	prev := ch.Room().Id
	if prev != NoRoom {
		if room, ok = b.rooms[prev]; ok {
			room.Leave(ch)
		}
		if room, ok = b.rooms[Plaza]; !ok {
			room = NewRoom(Plaza)
			b.rooms[Plaza] = room
		}
		ch.Room(room)
		if room != nil {
			err = room.Join(ch)
		}
	}
	return
}

func (b *Bucket) DeleteChannel(ch IChannel) {
	// 注意：这里必须持有写锁。删除 chs/rooms map 项是写操作，
	// 在 RLock 下执行会与其他写者并发读写 map，导致 fatal error: concurrent map writes。
	b.cLock.Lock()
	defer b.cLock.Unlock()
	if cur, ok := b.chs[ch.UserId()]; ok {
		room := cur.Room()
		// delete from bucket
		delete(b.chs, ch.UserId())
		// 房间清空后解散并回收：Leave 返回 Drop（空且非 Plaza）
		if room != nil && room.Leave(cur) && room.Drop {
			delete(b.rooms, room.Id)
		}
	}
}

func (b *Bucket) Channel(userId int64) (ch IChannel) {
	b.cLock.RLock()
	defer b.cLock.RUnlock()
	ch = b.chs[userId]
	return
}

// BroadcastRoom 向 worker 池投递房间广播请求（异步），返回是否投递成功。
// 投递非阻塞：队列满时返回 false（广播是尽力而为，不应让调用方被队列阻塞），
// 由调用方决定降级（如同步直推）或忽略。
func (b *Bucket) BroadcastRoom(pushRoomMsgReq *message.PushRoomMsgRequest) bool {
	num := b.routinesNum.Add(1) % b.RoutineAmount
	select {
	case b.routines[num] <- pushRoomMsgReq:
		return true
	default:
		// 队列满说明下游消费不过来。日志走标准库 log 包（全局互斥锁），
		// 若逐条打印会在大流量下形成日志风暴并反噬热路径，故限流：
		// 仅首次与每满 1024 次丢弃打一条，携带累计计数。
		if n := b.dropped.Add(1); n == 1 || n%1024 == 0 {
			log.Printf("bucket broadcast room queue full, room:%d dropped(total:%d)", pushRoomMsgReq.RoomId, n)
		}
		return false
	}
}
