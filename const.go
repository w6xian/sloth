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
	// KCP 基于 UDP 的 KCP（自带 BlockCrypt 加密，不需要 TLS）
	KCP = "kcp"
	// QUIK 是 QUIC 的拼写兼容别名（QUIC 不是缩写，正确写法是 QUIC）。
	// 保留它只为兼容早期拼写，新代码请用 QUIC。
	QUIK = QUIC
)

// HeaderProtocol 固定请求头键：标明这条调用是从哪条协议上发出来的
// （取值就是上面的 ws / tcp / quic）。
//
// 由客户端库在每次调用时自动写入（见 ServerRpc.callHeader），业务代码不用、
// 也不该手填：它是连接属性而不是业务参数。服务端用 GetProtocol(ctx) 读取。
//
// 为什么需要它：同一个服务同时监听多条协议时，进来的调用长得一模一样
// （方法名、入参都相同），服务端无从分辨对端跑在哪条链路上。靠业务自己
// 往入参里塞协议名是不可靠的——不同客户端传同一个值时（例如都传 "sign"），
// 三条链路在服务端会被登记成同一个身份，推送日志里再也分不出消息给了谁。
const HeaderProtocol = "sloth-protocol"
