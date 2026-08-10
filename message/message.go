package message

import (
	"context"
)

type JsonCallObject struct {
	Header map[string]string `json:"header,omitempty"` // header
	Method string            `json:"method"`           // service method name
	Args   [][]byte          `json:"args,omitempty"`   // args
	Data   []byte            `json:"data,omitempty"`   // hidden arg
	Error  string            `json:"error,omitempty"`  // error message
}

type JsonBackObject struct {
	Context context.Context   `json:"-"`
	Header  map[string]string `json:"header,omitempty"` // header
	//data binary body bytes
	Data []byte `json:"data,omitempty"`
	// error
	Error string `json:"error,omitempty"` // error message
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
