package sloth

type Decoder func([]byte) ([]byte, error)
type Encoder func(any) ([]byte, error)

// 传输层协议名：可直接作为 Listen / Dial 的 network 参数。
//
//	conn.Listen(ctx, sloth.QUIC, "localhost:8992")
//	conn.Dial(ctx, sloth.TCP, "localhost:8991")
//
// 之所以是无类型的字符串常量（而不是 types.Protocol 那样的具名类型）：
// network 参数的类型是 string，用无类型常量可以直接传，不必到处 string(...) 转换。
// 具名的那一套仍在（ProtocolTCP / ProtocolQUIC ...），供需要类型约束的场景使用。
const (
	// WEBSOCKET WebSocket（明文）
	WEBSOCKET = "ws"
	// WS 是 WEBSOCKET 的简写别名
	WS = "ws"
	// WSS WebSocket over TLS
	WSS = "wss"
	// TCP 裸 TCP 字节流（FN 帧分帧）
	TCP = "tcp"
	// QUIC 基于 UDP 的 QUIC（强制 TLS）
	QUIC = "quic"
	// QUIK 是 QUIC 的拼写兼容别名（QUIC 不是缩写，正确写法是 QUIC）。
	// 保留它只为兼容早期拼写，新代码请用 QUIC。
	QUIK = QUIC
)
