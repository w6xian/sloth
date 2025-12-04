package message

import (
	"github.com/w6xian/sloth/actions"
	"github.com/w6xian/sloth/decoder"
)

type JsonCallObject struct {
	Id     uint64 `json:"id"`              // user id
	Action int    `json:"action"`          // operation for request
	Method string `json:"method"`          // service method name
	Data   string `json:"data"`            // binary body bytes
	Error  string `json:"error,omitempty"` // error message
}

func NewWsJsonCallObject(method string, data []byte) *JsonCallObject {
	return &JsonCallObject{
		Id:     decoder.NextId(),
		Action: actions.ACTION_CALL,
		Method: method,
		Data:   string(data),
	}
}

type JsonBackObject struct {
	Id   uint64 `json:"id"` // user id
	Type int    `json:"-"`  // message type 1 textMessage or 2 binaryMessage1
	// action
	Action int64 `json:"action"`
	//data binary body bytes
	Data any `json:"data,omitempty"`
	// error
	Error string `json:"error,omitempty"` // error message
}

func NewWsJsonBackSuccess(id uint64, data []byte, msgType ...int) *JsonBackObject {
	//判断 msgType 是不是二进制
	msgTypeVal := TextMessage
	if len(msgType) > 0 {
		msgTypeVal = msgType[0]
	}
	if msgTypeVal != TextMessage && msgTypeVal != BinaryMessage {
		msgTypeVal = TextMessage
	}
	rst := &JsonBackObject{
		Id:     id,
		Action: actions.ACTION_REPLY,
		Type:   msgTypeVal,
	}

	if data != nil {
		rst.Data = data
	}
	return rst
}
func NewWsJsonBackError(id uint64, err []byte) *JsonBackObject {
	rst := &JsonBackObject{
		Id:     id,
		Action: actions.ACTION_REPLY,
	}
	if err != nil {
		rst.Type = TextMessage
		rst.Error = string(err)
	}
	return rst
}

const (
	TextMessage   = 1
	BinaryMessage = 2
)

type Msg struct {
	Type int `json:"type"` // message type 1 textMessage or 2 binaryMessage1
	// SeqId     string `json:"seq"`  // sequence number chosen by client
	Body []byte `json:"body"` // binary body bytes
}

func NewTextMessage(body []byte) *Msg {
	return &Msg{
		Type: TextMessage,
		// SeqId:     "0",
		Body: body,
	}
}

func NewBinaryMessage(body []byte) *Msg {
	return &Msg{
		Type: BinaryMessage,
		// SeqId:     "0",
		Body: body,
	}
}

func NewMessage(msgType int, body []byte) *Msg {
	if msgType != TextMessage && msgType != BinaryMessage {
		msgType = TextMessage
	}
	return &Msg{
		Type: msgType,
		Body: body,
	}
}

type JsonCallMsg struct {
	Id     string // user id
	Method string // service method name
	Args   any    // binary body bytes
	Reply  any    // binary body bytes
}

type PushMsgRequest struct {
	UserId int
	Msg    *Msg
}

type PushRoomMsgRequest struct {
	RoomId int64
	Msg    *Msg
}

type PushRoomCountRequest struct {
	RoomId int64
	Count  int
}
