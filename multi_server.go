package sloth

import (
	"context"
	"errors"
	"sync"

	"github.com/w6xian/sloth/v4/bucket"
	"github.com/w6xian/sloth/v4/message"
	"github.com/w6xian/sloth/v4/types"
)

// multiServer 把同一 Connect 上的多个传输实例合成一个 types.IServer。
//
// 为什么需要它：Connect.Listen 可以注册多条协议（ws + tcp + quic 同时开），
// 但 ClientRpc（服务端主动推送：Call / CallRoom / CallBucket / Broadcast）
// 只有一个 Serve 字段。此前每次 Listen 都直接覆盖它，结果是**只有最后注册的
// 那个协议**的连接能被推送触达——先注册的协议照样收包、照样能调用服务端，
// 却永远收不到服务端主动推下来的消息，且不报任何错。
//
// 合成实例对外仍是 types.IServer（接口不变），只是把"找连接 / 找房间 / 广播"
// 这几个动作扇出到所有传输上：连接分桶是每个传输各自持有的（ws 连接在 ws 的
// 桶里，tcp 连接在 tcp 的桶里），所以查找必须跨传输逐个问，不能先选桶。
type multiServer struct {
	mu      sync.RWMutex
	servers []types.IServer
}

func newMultiServer(servers ...types.IServer) *multiServer {
	m := &multiServer{}
	for _, s := range servers {
		m.add(s)
	}
	return m
}

// add 登记一个传输实例；重复的实例（同一协议多条监听地址共用一个实例）只记一次。
func (m *multiServer) add(s types.IServer) {
	if s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.servers {
		if old == s {
			return
		}
	}
	m.servers = append(m.servers, s)
}

// list 取当前实例快照：扇出过程中不持锁，避免调用传输的 Broadcast（内部会入队、
// 可能阻塞）时把 Listen / Close 一起堵住。
func (m *multiServer) list() []types.IServer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]types.IServer(nil), m.servers...)
}

// ChannelOf 跨所有传输找该用户的连接。
//
// 它是**可选能力**（不在 types.IServer 里），由 ClientRpc 通过类型断言使用：
// 单传输时走原来的 Bucket().Channel() 路径，行为完全不变。
func (m *multiServer) ChannelOf(userId int64) bucket.IChannel {
	for _, s := range m.list() {
		if ch := s.Channel(userId); ch != nil {
			return ch
		}
	}
	return nil
}

// Channel 实现 types.IServer：语义与 ChannelOf 一致（跨传输查找）。
func (m *multiServer) Channel(userId int64) bucket.IChannel { return m.ChannelOf(userId) }

// Bucket 返回该用户**实际所在**的桶。
//
// 用户不在任何传输上时退化为第一个传输的桶：单传输语义下 Bucket 永远非 nil，
// 调用方（业务代码里的 svr.Bucket(uid).Put(...)）依赖这一点。
func (m *multiServer) Bucket(userId int64) *bucket.Bucket {
	var first *bucket.Bucket
	for _, s := range m.list() {
		b := s.Bucket(userId)
		if b == nil {
			continue
		}
		if first == nil {
			first = b
		}
		if b.Channel(userId) != nil {
			return b
		}
	}
	return first
}

// Room 返回第一个命中的房间分片（保持 types.IServer 的单值语义）。
func (m *multiServer) Room(roomId int64) *bucket.Room {
	for _, s := range m.list() {
		if r := s.Room(roomId); r != nil {
			return r
		}
	}
	return nil
}

// Rooms 返回该房间在所有传输、所有分片上的 Room 对象。
//
// 房间成员按 userId 分散在各分片与各传输里，只取一个会漏人
// （表现为"房间广播有人收不到"），所以这里必须全量收集。
func (m *multiServer) Rooms(roomId int64) []*bucket.Room {
	var rooms []*bucket.Room
	for _, s := range m.list() {
		if v, ok := any(s).(interface{ Rooms(int64) []*bucket.Room }); ok {
			rooms = append(rooms, v.Rooms(roomId)...)
			continue
		}
		if r := s.Room(roomId); r != nil && !r.IsDrop() {
			rooms = append(rooms, r)
		}
	}
	return rooms
}

// AllBuckets 汇总所有传输的分片（全服遍历用）。
func (m *multiServer) AllBuckets() []*bucket.Bucket {
	var all []*bucket.Bucket
	for _, s := range m.list() {
		all = append(all, s.AllBuckets()...)
	}
	return all
}

// Broadcast 向所有传输广播：单个传输失败不影响其余传输。
func (m *multiServer) Broadcast(ctx context.Context, msg *message.Msg) error {
	var errs []error
	for _, s := range m.list() {
		if err := s.Broadcast(ctx, msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// 编译期断言：合成实例必须满足上层要求的接口集合。
var (
	_ types.IServer = (*multiServer)(nil)
	_ interface {
		Rooms(int64) []*bucket.Room
	} = (*multiServer)(nil)
	_ interface {
		ChannelOf(int64) bucket.IChannel
	} = (*multiServer)(nil)
)
