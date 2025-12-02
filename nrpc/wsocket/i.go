package wsocket

import (
	"net/http"
	"sloth/group"
)

type IWsReply interface {
	Reply(id string, data []byte) (err error)
}

type IServerHandleMessage interface {
	HandleMessage(s *WsServer, ch group.IChannel, msgType int, message []byte) error
}
type IClientHandleMessage interface {
	HandleMessage(c *LocalClient, ch *WsClient, msgType int, message []byte) error
}

type ISock interface {
	HandleFunc(path string, handler func(w http.ResponseWriter, r *http.Request))
}
