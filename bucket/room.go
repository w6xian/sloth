package bucket

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/w6xian/sloth/v3/message"
)

const NoRoom = -1
const Plaza = 0

// Room 房间：管理一组 IChannel 成员。
//
// 设计说明：
//   - 成员使用 map[IChannel]struct{} 存储（而非侵入式双向链表），增删 O(1)、遍历 O(n)，
//     天然支持同一个 IChannel 同时加入多个 Room（每个 Room 独立维护自己的成员集合，
//     channel 自身不再需要被房间占用 next/prev 指针）。
//   - 遍历采用"先快照、后回调"策略：在写锁之外拷贝成员列表再逐个回调，
//     回调中可安全调用 Join/Leave，不会死锁，也不会遗漏遍历期间新加入的成员。
//   - 房间清空且非广场(Plaza)时标记 Drop（解散），由上层（Bucket）回收。
//   - 不需要队列：成员管理由 map 承担；广播通过 ch.Push 已进入各 channel 的
//     缓冲通道实现异步削峰，Room 内再引入队列只会增加延迟与内存开销。
type Room struct {
	Id   int64
	Drop bool // 房间是否已解散（空房间置 true，Plaza 除外）

	rLock    sync.RWMutex
	channels map[IChannel]struct{}
}

func NewRoom(roomId int64) *Room {
	return &Room{
		Id:       roomId,
		channels: make(map[IChannel]struct{}),
	}
}

// Join 进入房间。
// 同一个 channel 可加入多个房间；重复加入同一房间是幂等的。
// 房间已解散(Drop)时返回错误。
func (r *Room) Join(ch IChannel) error {
	r.rLock.Lock()
	defer r.rLock.Unlock()
	if r.Drop {
		return errors.New("room drop")
	}
	r.channels[ch] = struct{}{}
	return nil
}

// Put 是 Join 的别名，保留旧 API 兼容。
//
// Deprecated: 请使用 Join。
func (r *Room) Put(ch IChannel) error { return r.Join(ch) }

// Leave 退出房间，返回房间是否已解散（空且非广场）。
// channel 不在房间中时直接返回当前 Drop 状态，不影响其他成员。
func (r *Room) Leave(ch IChannel) bool {
	r.rLock.Lock()
	defer r.rLock.Unlock()
	if _, ok := r.channels[ch]; !ok {
		return r.Drop
	}
	delete(r.channels, ch)
	if len(r.channels) == 0 && r.Id != Plaza {
		r.Drop = true
	}
	return r.Drop
}

// DeleteChannel 是 Leave 的别名，保留旧 API 兼容。
//
// Deprecated: 请使用 Leave。
func (r *Room) DeleteChannel(ch IChannel) bool { return r.Leave(ch) }

// Contains 判断 channel 是否在房间中。
func (r *Room) Contains(ch IChannel) bool {
	r.rLock.RLock()
	defer r.rLock.RUnlock()
	_, ok := r.channels[ch]
	return ok
}

// Len 返回房间在线成员数。
func (r *Room) Len() int {
	r.rLock.RLock()
	defer r.rLock.RUnlock()
	return len(r.channels)
}

// IsDrop 返回房间是否已解散。
func (r *Room) IsDrop() bool {
	r.rLock.RLock()
	defer r.rLock.RUnlock()
	return r.Drop
}

// Each 遍历房间内所有成员。
// 回调在锁外执行，fn 中可安全调用 Join/Leave；遍历顺序随机（map 语义）。
func (r *Room) Each(fn func(ch IChannel)) {
	r.rLock.RLock()
	chs := make([]IChannel, 0, len(r.channels))
	for ch := range r.channels {
		chs = append(chs, ch)
	}
	r.rLock.RUnlock()
	for _, ch := range chs {
		fn(ch)
	}
}

// Range 遍历房间内所有成员，fn 返回 false 时提前终止。
func (r *Room) Range(fn func(ch IChannel) bool) {
	r.rLock.RLock()
	chs := make([]IChannel, 0, len(r.channels))
	for ch := range r.channels {
		chs = append(chs, ch)
	}
	r.rLock.RUnlock()
	for _, ch := range chs {
		if !fn(ch) {
			return
		}
	}
}

// EachBatch 分批遍历房间成员：把快照切成长度不超过 batchSize 的批次，逐批回调。
// 相比 Each：可在每批之间做进度统计、失败重试，或对批次内部做并发控制，
// 适合超大房间的"分批下发"场景。
func (r *Room) EachBatch(batchSize int, fn func(chs []IChannel)) {
	if batchSize <= 0 {
		batchSize = 1
	}
	r.rLock.RLock()
	chs := make([]IChannel, 0, len(r.channels))
	for ch := range r.channels {
		chs = append(chs, ch)
	}
	r.rLock.RUnlock()

	for i := 0; i < len(chs); i += batchSize {
		end := i + batchSize
		if end > len(chs) {
			end = len(chs)
		}
		fn(chs[i:end])
	}
}

// Broadcast 向房间内所有成员广播消息。
// ch.Push 内部为缓冲通道，此处不做阻塞等待，天然异步削峰。
func (r *Room) Broadcast(ctx context.Context, msg *message.Msg) {
	r.Each(func(ch IChannel) {
		if err := ch.Push(ctx, msg); err != nil {
			log.Printf("room broadcast err:%s", err.Error())
		}
	})
}

// Push 是 Broadcast 的别名，保留旧 API 兼容。
//
// Deprecated: 请使用 Broadcast。
func (r *Room) Push(ctx context.Context, msg *message.Msg) {
	r.Broadcast(ctx, msg)
}
