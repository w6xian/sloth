package option

import (
	"time"

	"github.com/gorilla/mux"
)

type Options struct {
	// ReadWait is the duration for which the server allows a client to read a message.
	ReadWait time.Duration
	// WriteWait is the duration for which the server allows a client to write a message.
	WriteWait time.Duration
	// PongWait is the duration for which the server allows a client to send a pong message.
	PongWait time.Duration
	// PingPeriod is the duration for which the server sends ping messages to the client.
	PingPeriod time.Duration
	// MaxMessageSize is the maximum size of a message that the server allows to be received.
	MaxMessageSize int64
	// ReadBufferSize is the size of the buffer used to read messages from the client.
	ReadBufferSize int
	// WriteBufferSize is the size of the buffer used to write messages to the client.
	WriteBufferSize int
	// BroadcastSize is the size of the buffer used to broadcast messages to all clients.
	BroadcastSize int
	// ChannelSize is the size of the channel used to store messages for each client.
	ChannelSize int
	// RoomSize is the size of the room used to store messages for each room.
	RoomSize int
	// RoutineAmount is the amount of goroutines used to handle messages.
	RoutineAmount uint64
	// RoutineSize is the size of the buffer used to store messages for each goroutine.
	RoutineSize int
	// SliceSize is the size of the slice used to store messages for each client.
	SliceSize int64
	// KeepAlive is the duration for which the server allows a client to keep the connection alive.
	KeepAlive bool
}

func NewOptions() *Options {
	return &Options{
		ReadWait:        10 * time.Second,
		WriteWait:       10 * time.Second,
		PongWait:        54 * time.Second,
		PingPeriod:      (54 * time.Second * 9) / 10,
		MaxMessageSize:  1024 * 1024,
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		BroadcastSize:   512,
		/*Bucket option*/
		ChannelSize:   1024,
		RoomSize:      1024,
		RoutineAmount: 32,
		RoutineSize:   20,
		SliceSize:     512,
		KeepAlive:     true,
	}
}

type IConnectOption interface {
	SetUriPath(path string) error
	SetRouter(router *mux.Router) error
	SetAddress(address string) error
	SetServerHandleMessage(handler IServerHandleMessage) error
	SetClientHandleMessage(handler IClientHandleMessage) error
}

type ConnectOption func(s IConnectOption)

func WithUriPath(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetUriPath(path)
	}
}

func WithAddress(path string) ConnectOption {
	return func(s IConnectOption) {
		s.SetAddress(path)
	}
}

func WithRouter(router *mux.Router) ConnectOption {
	return func(s IConnectOption) {
		s.SetRouter(router)
	}
}

func WithServerHandleMessage(handler IServerHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetServerHandleMessage(handler)
	}

}

func WithClientHandleMessage(handler IClientHandleMessage) ConnectOption {
	return func(s IConnectOption) {
		s.SetClientHandleMessage(handler)
	}

}
