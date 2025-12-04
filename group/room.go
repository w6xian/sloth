package group

import (
	"context"
	"fmt"
	"sync"

	"github.com/pkg/errors"
	"github.com/w6xian/sloth/message"
)

const NoRoom = -1
const Plaza = 0

type Room struct {
	Id          int64
	OnlineCount int // room online user count
	rLock       sync.RWMutex
	Drop        bool // make room is live
	Next        IChannel
}

func NewRoom(roomId int64) *Room {
	room := new(Room)
	room.Id = roomId
	room.Drop = false
	room.Next = nil
	room.OnlineCount = 0
	return room
}

func (r *Room) Put(ch IChannel) (err error) {
	//doubly linked list
	r.rLock.Lock()
	defer r.rLock.Unlock()
	if !r.Drop {
		if r.Next != nil {
			r.Next.Prev(ch)
		}
		ch.Next(r.Next)
		ch.Prev(nil)
		r.Next = ch
		r.OnlineCount++
	} else {
		err = errors.New("room drop")
	}
	return
}

func (r *Room) Push(ctx context.Context, msg *message.Msg) {
	r.rLock.RLock()
	defer r.rLock.RUnlock()
	// 从第一个用户开始推送
	var firstUserId int64
	ch := r.Next
	if ch != nil {
		firstUserId = ch.UserId()
		if err := ch.Push(ctx, msg); err != nil {
			fmt.Printf("push msg err:%s", err.Error())
		}
	}
	for ch = ch.Next(); ch != nil; ch = ch.Next() {
		if r.Drop {
			break
		}
		fmt.Println("Push", ch.UserId())
		if firstUserId == ch.UserId() {
			// 重复用户，不推送。防止出现重复推送
			fmt.Println("重复用户，不推送。防止出现重复推送")
			break
		}
		if err := ch.Push(ctx, msg); err != nil {
			fmt.Printf("push msg err:%s", err.Error())
		}
	}
}

func (r *Room) DeleteChannel(ch IChannel) bool {
	r.rLock.RLock()
	defer r.rLock.RUnlock()
	if ch.Next() != nil {
		//if not footer
		ch.Next().Prev(ch.Prev())
	}
	if ch.Prev() != nil {
		// if not header
		ch.Prev().Next(ch.Next())
	} else {
		r.Next = ch.Next()
	}
	r.OnlineCount--
	r.Drop = false
	if r.OnlineCount <= 0 {
		if r.Id != Plaza {
			r.Drop = true
		} else {
			r.OnlineCount = 0
		}
	}
	return r.Drop
}
