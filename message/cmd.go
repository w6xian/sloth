package message

import "github.com/w6xian/sloth/internal/utils"

type CmdReq struct {
	Id        string `json:"id"`
	TrackId   string `json:"track_id"`
	AppId     string `json:"app_id,omitempty"`
	ProxyId   int64  `json:"proxy_id,omitempty"`
	RoomId    int64  `json:"room_id,omitempty"`
	UserId    int64  `json:"user_id,omitempty"`
	Action    int    `json:"action"` // 操作类型
	AuthToken string `json:"auth_token,omitempty"`
	Data      string `json:"data,omitempty"` // 操作数据
	Lang      string `json:"lang,omitempty"`
	Method    string `json:"method,omitempty"` // service method name
	Ts        int64  `json:"ts,omitempty"`
}

func (c *CmdReq) Bytes() []byte {
	return []byte(utils.JsonString(c))
}

type DisConnectRequest struct {
	RoomId int64
	UserId int64
}
