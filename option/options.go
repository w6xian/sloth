package option

import "time"

type Options struct {
	// ReadWait is the duration for which the server allows a client to read a message.
	ReadWait time.Duration
	// WriteWait is the duration for which the server allows a client to write a message.
	WriteWait time.Duration
	// PongWait is the duration for which the server allows a client to send a pong message.
	PongWait time.Duration
	// PingPeriod is the duration for which the server sends ping messages to the client.
	PingPeriod time.Duration
	// MaxMessageSize is the maximum size of a message that the server allows to be received.
	MaxMessageSize int64
	// ReadBufferSize is the size of the buffer used to read messages from the client.
	ReadBufferSize int
	// WriteBufferSize is the size of the buffer used to write messages to the client.
	WriteBufferSize int
	// BroadcastSize is the size of the buffer used to broadcast messages to all clients.
	BroadcastSize int
	// ChannelSize is the size of the channel used to store messages for each client.
	ChannelSize int
	// RoomSize is the size of the room used to store messages for each room.
	RoomSize int
	// RoutineAmount is the amount of goroutines used to handle messages.
	RoutineAmount uint64
	// RoutineSize is the size of the buffer used to store messages for each goroutine.
	RoutineSize int
	// SliceSize is the size of the slice used to store messages for each client.
	SliceSize int64
	// KeepAlive is the duration for which the server allows a client to keep the connection alive.
	KeepAlive bool

	// WorkerNum 处理入站消息的 worker 数量。每条连接按哈希固定到某个 worker，
	// 因此同连接严格按序、不同连接并行。0 表示按 GOMAXPROCS 自动（最少 4）。
	WorkerNum int
	// WorkerQueueSize 每个 worker 的待处理队列长度；队列满时投递阻塞（背压）。
	// 0 表示默认 256。
	WorkerQueueSize int
	// ChannelQueueSize 每条连接的各队列容量（待发 RPC / 回包 / 广播）。
	// 决定对端处理不过来时能缓冲多少：太小→突发时频繁队列满，
	// 太大→内存占用高且延迟被掩盖。0 表示默认 10。
	ChannelQueueSize int

	MaxConnsGlobal int64
	MaxConnsPerIP  int64
	MaxConnsWS     int64
	MaxConnsTCP    int64
	MaxConnsQUIC   int64
	// MaxConnsKCP 为 KCP 预留：协议尚未实现，设了也不会生效。
	//
	// 分协议限额与全局限额是**两道独立的闸**（取更严的那个），不是"加起来"：
	// 每种传输的 server 实例只服务一种协议，因此它的连接数既是全局的一份，
	// 也要受本传输上限约束。单 IP 限额（MaxConnsPerIP）目前只在 ws 生效——
	// 它依赖 HTTP 请求头取 IP，TCP/QUIC 要另做按 IP 计数。
	MaxConnsKCP int64
	// TrustProxyHeaders 为 true 时，从 X-Forwarded-For / X-Real-IP 解析客户端 IP，
	// 用于 MaxConnsPerIP 计数。仅在可信反向代理之后部署时开启，否则可被伪造绕过。
	TrustProxyHeaders bool

	TLSCertFile string
	TLSKeyFile  string

	// DebugAddr 非空时，Serve() 会在该地址独立启动调试服务：
	// /debug/metrics（Prometheus）、/debug/pprof/*、/debug/vars。
	// 建议填内网/回环地址（如 127.0.0.1:6060）：pprof 无鉴权，暴露公网等于开放堆采样。
	DebugAddr string
	// LogLevel 日志级别名：debug/info/warn/error，为空表示 info。
	// 亦可用环境变量 SLOTH_LOG_LEVEL 覆盖（见 logger.ParseLevel）。
	LogLevel string
}

func NewOptions() *Options {
	return &Options{
		ReadWait:        10 * time.Second,
		WriteWait:       10 * time.Second,
		PongWait:        54 * time.Second,
		PingPeriod:      (54 * time.Second * 9) / 10,
		MaxMessageSize:  1024 * 1024,
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		BroadcastSize:   512,
		/*Bucket option*/
		ChannelSize:   1024,
		RoomSize:      1024,
		RoutineAmount: 32,
		RoutineSize:   20,
		SliceSize:     512,
		KeepAlive:     true,
		MaxConnsGlobal:    0,
		MaxConnsPerIP:     0,
		MaxConnsWS:        0,
		MaxConnsTCP:       0,
		MaxConnsQUIC:      0,
		MaxConnsKCP:       0,
		TrustProxyHeaders: false,
		TLSCertFile:       "",
		TLSKeyFile:        "",
	}
}
