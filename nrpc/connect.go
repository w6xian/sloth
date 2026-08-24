package nrpc

import "context"

type IDataHandler interface {
	Send(ctx context.Context, id uint64, payload []byte, err error) error
	Receive(ctx context.Context, payload []byte) error
}

type IReadConn interface {
	ReadMessage() (int, []byte, error)
}
