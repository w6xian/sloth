package actions

const (
	// 需要返回的操作
	ACTION_CALL  = -0xFF
	ACTION_REPLY = -0xFE // 别名
	// 无效操作
	ACTION_INVALID = 0x00
	//广播
	ACTION_BROADCAST = 0xFF
)
