package message

import (
	"github.com/w6xian/sloth/actions"
	"github.com/w6xian/sloth/decoder"
	"github.com/w6xian/sloth/internal/utils"
)

type JsonCallObject struct {
	Id     uint64   `json:"id"`              // user id
	Action int      `json:"action"`          // operation for request
	Type   int      `json:"type"`            // message type 1 textMessage or 2 binaryMessage1
	Method string   `json:"method"`          // service method name
	Data   []byte   `json:"data,omitempty"`  // binary body bytes
	Error  string   `json:"error,omitempty"` // error message
	Args   [][]byte `json:"args,omitempty"`  // args
}

func NewWsJsonCallObject(method string, data ...[]byte) *JsonCallObject {
	//判断 msgType 是不是二进制
	msgTypeVal := TextMessage
	arg := []byte{}
	if len(data) > 0 {
		arg = data[0]
	}
	args := [][]byte{}
	if len(data) > 1 {
		args = data[1:]
	}
	// fmt.Println("NewWsJsonCallObject args:", arg, args)
	return &JsonCallObject{
		Id:     decoder.NextId(),
		Action: actions.ACTION_CALL,
		Type:   msgTypeVal,
		Method: method,
		Data:   arg,
		Args:   args,
	}
}

func (j *JsonCallObject) IsBinary() bool {
	return j.Type == BinaryMessage
}

// 转成二进制消息
func (j *JsonCallObject) ToBytes() []byte {
	if j.IsBinary() {
		return utils.Serialize(j.Data)
	}
	return utils.Serialize(j)
}

type IJsonCallObject interface {
	Id() uint64
	Action() int
	Method() string
	Data() []byte
	Error() string
}

type JsonBackObject struct {
	Id   uint64 `json:"id"` // user id
	Type int    `json:"-"`  // message type 1 textMessage or 2 binaryMessage1
	// action
	Action int64 `json:"action"`
	//data binary body bytes
	Data []byte `json:"data,omitempty"`
	// error
	Error string   `json:"error,omitempty"` // error message
	Args  [][]byte `json:"args,omitempty"`  // args
}

func (j *JsonBackObject) IsBinary() bool {
	return j.Type == BinaryMessage
}

// 转成二进制消息
func (j *JsonBackObject) ToBytes() []byte {
	if j.IsBinary() {
		return utils.Serialize(j.Data)
	}
	return utils.Serialize(j)
}

func NewWsJsonBackSuccess(id uint64, data []byte) *JsonBackObject {
	//判断 msgType 是不是二进制
	msgTypeVal := TextMessage

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
