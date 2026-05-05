package auth

type AuthInfo struct {
	UserId int64  `json:"user_id"`
	RoomId int64  `json:"room_id"`
	Token  string `json:"token"`
}
