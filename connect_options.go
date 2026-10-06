package sloth

import (
	"context"
	"crypto/tls"
	"time"
)

// connect options

type IRpcOption func(IRpc)
type ServerRpcOption func(*ServerRpc)
type ClientRpcOption func(*ClientRpc)
type ConnectProxy func(ctx context.Context, service string) (int64, error)

// 链接相关参数，透传到ws中
type ConnOption func(*Connect)

func WithSleepTimes(times int) ConnOption {
	return func(c *Connect) {
		c.sleepTimes = times
	}
}

func WithTimes(times int) ConnOption {
	return func(c *Connect) {
		c.times = times
	}
}

func WithCpuNum(cpuNum int) ConnOption {
	return func(c *Connect) {
		c.cpuNum = cpuNum
	}
}

func WithClientLogic(l *ClientRpc) ConnOption {
	return func(c *Connect) {
		c.client = l
	}
}

func Client(l *ClientRpc) ConnOption {
	return func(c *Connect) {
		c.client = l
	}
}

func Server(l *ServerRpc) ConnOption {
	return func(c *Connect) {
		c.server = l
	}
}

func WithPongTimeout(timeout time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.PongWait = timeout
	}
}

// readWait default eq 10s
func WithReadWait(readWait time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.ReadWait = readWait
	}
}

func WithWriteWait(writeWait time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.WriteWait = writeWait
	}
}

func WithMaxMessageSize(maxMessageSize int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxMessageSize = maxMessageSize
	}
}

func WithPingPeriod(pingPeriod time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.PingPeriod = pingPeriod
	}
}

func WithConnectProxy(proxyHandler ConnectProxy) ConnOption {
	return func(ch *Connect) {
		ch.proxyHandler = proxyHandler
	}
}

/*
 bucket options
*/
// ChannelSize   int
// 	RoomSize      int
// 	RoutineAmount uint64
// 	RoutineSize   int

func WithChannelSize(channelSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.ChannelSize = channelSize
	}
}

func WithRoomSize(roomSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoomSize = roomSize
	}
}
func WithRoutineAmount(routineAmount uint64) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoutineAmount = routineAmount
	}
}

func WithRoutineSize(routineSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoutineSize = routineSize
	}
}

func WithMaxConnsGlobal(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsGlobal = max
	}
}

func WithMaxConnsPerIP(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsPerIP = max
	}
}

func WithMaxConnsWS(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsWS = max
	}
}

func WithMaxConnsTCP(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsTCP = max
	}
}

func WithMaxConnsQUIC(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsQUIC = max
	}
}

func WithMaxConnsKCP(max int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxConnsKCP = max
	}
}

func WithTrustProxyHeaders(trust bool) ConnOption {
	return func(ch *Connect) {
		ch.Option.TrustProxyHeaders = trust
	}
}

// WithDebugAddr 指定调试服务的独立监听地址（如 "127.0.0.1:6060"）。
// Serve() 会在该地址启动 /debug/metrics、/debug/pprof/*、/debug/vars。
// 仅监听内网或回环地址：这些端点无鉴权。
func WithDebugAddr(addr string) ConnOption {
	return func(ch *Connect) {
		ch.Option.DebugAddr = addr
	}
}

// WithLogLevel 设置日志级别：debug/info/warn/error。
// 亦可用环境变量 SLOTH_LOG_LEVEL 设置（显式选项优先）。
func WithLogLevel(level string) ConnOption {
	return func(ch *Connect) {
		ch.Option.LogLevel = level
	}
}

func WithTLSCertFile(path string) ConnOption {
	return func(ch *Connect) {
		ch.Option.TLSCertFile = path
	}
}

func WithTLSKeyFile(path string) ConnOption {
	return func(ch *Connect) {
		ch.Option.TLSKeyFile = path
	}
}

func WithTLSCertKey(certFile string, keyFile string) ConnOption {
	return func(ch *Connect) {
		ch.Option.TLSCertFile = certFile
		ch.Option.TLSKeyFile = keyFile
	}
}

// WithTLSConfig 直接设置 TLS 配置。
//
// wss 只需证书文件路径（WithTLSCertKey）；QUIC 必须用这个：QUIC 的加密由
// TLS 1.3 承担，服务端要带证书、客户端要决定校验策略（是否信任自签证书），
// 两者都只能通过 *tls.Config 表达。
func WithTLSConfig(conf *tls.Config) ConnOption {
	return func(ch *Connect) {
		ch.tlsConfig = conf
	}
}

// 编码解码

func UseEncoder(encoder Encoder) IRpcOption {
	return func(ch IRpc) {
		ch.SetEncoder(encoder)
	}
}

func UseDecoder(decoder Decoder) IRpcOption {
	return func(ch IRpc) {
		ch.SetDecoder(decoder)
	}
}

// WithMaxParamSize 设这一侧**单个参数（AG 帧）的最大字节数**。
//
// 协议能表达到 1GB（ag.MaxAgDataSize，与 fn 帧对齐），那是"线格式能装多大"；
// 这里设的是部署策略——"本进程最多收多大"：
//   - 入站：长度字段超过它直接拒绝，不照对端声明的数字分配内存；
//   - 出站：编码阶段就报错，不会发出去白跑一趟再被对端拒。
//
// 用法（传给你这一侧的角色即可）：
//
//	server := sloth.DefaultServer(sloth.WithMaxParamSize(4<<20))
//	client := sloth.DefaultClient(sloth.WithMaxParamSize(4<<20))
//
// 只设一侧也生效：建连接时会同步到另一个 rpc 对象（入站与出站走的是不同对象，
// 只设一个会漏掉一半方向）。不设则沿用 ag 的进程级默认（1GB）。
func WithMaxParamSize(n int) IRpcOption {
	return func(ch IRpc) {
		if s, ok := ch.(maxParamSizeSetter); ok {
			s.SetMaxParamSize(n)
		}
	}
}

// maxParamSizeSetter 窄接口：只为让 WithMaxParamSize 不用改 IRpc 定义
// （改接口会波及所有 IRpc 实现方）。
type maxParamSizeSetter interface {
	SetMaxParamSize(int)
}

type maxParamSizeHolder interface {
	MaxParamSize() int
	SetMaxParamSize(int)
}

// syncMaxParamSize 把设过的单参数限制补到另一侧。
//
// 入站（Connect.CallFunc 解参）与出站（Call/CallRoom 编码）走的是两个不同的
// rpc 对象，WithMaxParamSize 只设一个就会漏掉一个方向。只补没设的那一边，
// 两边都显式设过则各保持原值（例如服务端对下游收紧、对上游放开）。
func syncMaxParamSize(a, b maxParamSizeHolder) {
	if a == nil || b == nil {
		return
	}
	switch {
	case a.MaxParamSize() > 0 && b.MaxParamSize() == 0:
		b.SetMaxParamSize(a.MaxParamSize())
	case b.MaxParamSize() > 0 && a.MaxParamSize() == 0:
		a.SetMaxParamSize(b.MaxParamSize())
	}
}

func Listen(network, address string) ConnOption {
	return func(ch *Connect) {

	}
}
