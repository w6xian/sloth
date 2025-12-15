/*
 * 用于存放用户连接的桶，每个桶有多个房间，每个房间有多个连接，每个连接有一个用户
 */
package bucket

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/w6xian/sloth/message"
)

type Bucket struct {
	cLock sync.RWMutex       // protect the channels for chs
	chs   map[int64]IChannel // map sub key to a channel

	rooms       map[int64]*Room // bucket room channels
	routines    []chan *message.PushRoomMsgRequest
	routinesNum uint64
	broadcast   chan []byte

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
	ctx := context.Background()
	for i := uint64(0); i < b.RoutineAmount; i++ {
		c := make(chan *message.PushRoomMsgRequest, b.RoutineSize)
		b.routines[i] = c
		go b.PushRoom(ctx, c)
	}
	return
}

func (b *Bucket) PushRoom(ctx context.Context, ch chan *message.PushRoomMsgRequest) {
	for {
		var (
			arg  *message.PushRoomMsgRequest
			room *Room
		)
		arg = <-ch
		if room = b.Room(arg.RoomId); room != nil {
			room.Push(ctx, arg.Msg)
		}
	}
}
func (b *Bucket) GetRooms() map[int64]*Room {
	return b.rooms
}

func (b *Bucket) Room(rid int64) (room *Room) {
	b.cLock.RLock()
	defer b.cLock.RUnlock()
	room = b.rooms[rid]
	return
}

func (b *Bucket) Put(userId int64, roomId int64, ch IChannel) (err error) {
	var (
		room *Room
		ok   bool
	)
	b.cLock.Lock()
	defer b.cLock.Unlock()
	// 原来有房间，先退出房间
	if ch.Room() != nil {
		if ch.Room().Id == roomId {
			return
		}
		ch.Room().DeleteChannel(ch)
	}
	// fmt.Println("Put userId:", userId, "roomId:", roomId)
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
	b.chs[userId] = ch
	// fmt.Println("Put room:", room)
	if room != nil {
		err = room.Put(ch)
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
			room.DeleteChannel(ch)
		}
		if room, ok = b.rooms[Plaza]; !ok {
			room = NewRoom(Plaza)
			b.rooms[Plaza] = room
		}
		ch.Room(room)
		if room != nil {
			err = room.Put(ch)
		}
	}
	return
}

func (b *Bucket) DeleteChannel(ch IChannel) {
	var (
		ok   bool
		room *Room
	)
	b.cLock.RLock()
	defer b.cLock.RUnlock()
	if ch, ok = b.chs[ch.UserId()]; ok {
		room = b.chs[ch.UserId()].Room()
		//delete from bucket
		delete(b.chs, ch.UserId())
	}
	if room != nil && room.DeleteChannel(ch) {
		// if room empty delete,will mark room.Drop is true
		if room.Drop {
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

func (b *Bucket) BroadcastRoom(pushRoomMsgReq *message.PushRoomMsgRequest) {
	num := atomic.AddUint64(&b.routinesNum, 1) % b.RoutineAmount
	b.routines[num] <- pushRoomMsgReq
}
