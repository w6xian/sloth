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

func Listen(network, address string) ConnOption {
	return func(ch *Connect) {

	}
}
