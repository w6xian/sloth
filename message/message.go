package message

import (
	"github.com/w6xian/sloth/actions"
	"github.com/w6xian/sloth/internal/utils/id"
)

type JsonCallObject struct {
	Id     string `json:"id"`              // user id
	Action int    `json:"action"`          // operation for request
	Method string `json:"method"`          // service method name
	Data   string `json:"data"`            // binary body bytes
	Error  string `json:"error,omitempty"` // error message
}

func NewWsJsonCallObject(method string, data []byte) *JsonCallObject {
	return &JsonCallObject{
		Id:     id.ShortID(),
		Action: actions.ACTION_CALL,
		Method: method,
		Data:   string(data),
	}
}

type JsonBackObject struct {
	Id string `json:"id"` // user id
	// action
	Action int64 `json:"action"`
	//data binary body bytes
	Data string `json:"data,omitempty"`
	// error
	Error string `json:"error,omitempty"` // error message
}

func NewWsJsonBackSuccess(id string, data []byte) *JsonBackObject {
	rst := &JsonBackObject{
		Id:     id,
		Action: actions.ACTION_REPLY,
	}
	if data != nil {
		rst.Data = string(data)
	}
	return rst
}
func NewWsJsonBackError(id string, err []byte) *JsonBackObject {
	rst := &JsonBackObject{
		Id:     id,
		Action: actions.ACTION_REPLY,
	}
	if err != nil {
		rst.Error = string(err)
	}
	return rst
}

type Msg struct {
	Ver       int    `json:"ver"`  // protocol version
	Operation int    `json:"op"`   // operation for request
	SeqId     string `json:"seq"`  // sequence number chosen by client
	Body      []byte `json:"body"` // binary body bytes
}

type JsonCallMsg struct {
	Id     string // user id
	Method string // service method name
	Args   any    // binary body bytes
	Reply  any    // binary body bytes
}

type PushMsgRequest struct {
	UserId int
	Msg    Msg
}

type PushRoomMsgRequest struct {
	RoomId int64
	Msg    Msg
}

type PushRoomCountRequest struct {
	RoomId int64
	Count  int
}
