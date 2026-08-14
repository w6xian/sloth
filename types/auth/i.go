package auth

import "encoding/json"

type AuthInfo struct {
	UserId int64  `json:"user_id"`
	RoomId int64  `json:"room_id"`
	Token  string `json:"token"`
	Ts     int64  `json:"ts"`
}

func (a *AuthInfo) Json() ([]byte, error) {
	return json.Marshal(a)
}
