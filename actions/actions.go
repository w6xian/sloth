package actions

const (
	// 需要返回的操作
	ACTION_CALL          byte = 0x01
	ACTION_REPLY_SUCCESS byte = 0x02 // 别名
	ACTION_REPLY_ERROR   byte = 0x03 // 别名
	// 无效操作
	ACTION_INVALID byte = 0x00
	//广播
	ACTION_BROADCAST byte = 0xFF
)
