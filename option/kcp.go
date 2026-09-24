package option

import (
	"fmt"

	"github.com/w6xian/sloth/v4/codec"
	"github.com/w6xian/sloth/v4/logger"
	"github.com/w6xian/sloth/v4/types/handler"

	"github.com/gorilla/mux"
)

// KCP 支持的载荷加密方式。
//
// KCP 把加密做在传输层（BlockCrypt 对载荷做块加密），**不需要 TLS**——
// 这与 QUIC 相反（QUIC 的加密由 TLS 1.3 承担，没有证书就握不上手）。
// 两端必须用同一种 crypt 与同一份 key，否则包解不开，表现为连不上。
const (
	KCPCryptNone     = "none"
	KCPCryptXOR      = "xor"
	KCPCryptTEA      = "tea"
	KCPCryptAES      = "aes"
	KCPCryptSalsa20  = "salsa20"
	KCPCryptBlowfish = "blowfish"
	KCPCryptSM4      = "sm4"
)

// KCPConfig KCP（UDP 之上）传输的参数。
//
// 放在 option 包而不是 nrpc/kcp：option 不能被传输包反向 import（会成环），
// 而 WithKCPConfig 要先持有配置类型才能构造 ConnectOption。这里只放数据，
// BlockCrypt 的构造与调优项的落地（依赖 kcp-go）在 nrpc/kcp 里完成。
//
// 整数项为 0 表示"保持 kcp-go 默认"，不调用对应的 Set 方法。
// 这样零值 KCPConfig{} 就是 kcp-go 原生行为，不会悄悄改变默认语义。
type KCPConfig struct {
	// Crypt 加密方式，见 KCPCrypt* 常量；空串按 none（不加密）处理。
	Crypt string
	// Key 加密密钥。长度要求随 Crypt 而变（aes 需 16/24/32 字节，
	// salsa20 需 32 字节），不满足时建连接会直接返回 error。
	Key []byte

	// FEC 前向纠错：用冗余包换取丢包下的延迟，代价是多耗带宽。
	// 两者都 > 0 才开启，典型取值 10 / 3（每 10 个包带 3 个冗余包）。
	DataShards   int
	ParityShards int

	// NoDelay / Interval / Resend / NC 对应 kcp-go 的 SetNoDelay：
	// 是否启用 nodelay、内部刷新间隔(ms)、快速重传模式、是否关闭拥塞控制。
	// 只有 NoDelay 与 Interval 同时 > 0 才会调用 SetNoDelay——
	// interval=0 会让 KCP 每收到一个包就 flush，等于空转刷 CPU。
	NoDelay  int
	Interval int
	Resend   int
	NC       int

	// SndWnd / RcvWnd 发送与接收窗口（包数）。任一项 > 0 才调用 SetWindowSize，
	// 另一项按 kcp-go 的默认窗口补齐。
	SndWnd int
	RcvWnd int

	// MTU 最大传输单元；0 表示用 kcp-go 默认（1400）。
	// 超过对端网络的 MTU 会触发 IP 分片，反而更容易丢包。
	MTU int

	// StreamMode 流模式：不再保留消息边界，允许把小包合并成一个 UDP 包发，
	// 吞吐更高。sloth 走的是字节流 + FN 分帧，本身不依赖消息边界，
	// 因此低延迟与高吞吐场景都建议设为 true。
	StreamMode bool
	// ACKNoDelay 收到包立即回 ACK（默认攒一批再回）：降低 RTT 判定延迟，
	// 代价是 ACK 流量翻倍。
	ACKNoDelay bool
}

// FastKCPConfig 低延迟预设：kcp-go 文档里的"极速模式"。
//
// 适用：实时性优先、能接受多耗一点带宽（关闭拥塞控制 + 立即 ACK）。
// 长肥管道或共享带宽环境慎用 NC=1（关拥塞控制）——会把带宽占满。
func FastKCPConfig() KCPConfig {
	return KCPConfig{
		NoDelay:    1,
		Interval:   10,
		Resend:     2,
		NC:         1,
		SndWnd:     128,
		RcvWnd:     128,
		StreamMode: true,
		ACKNoDelay: true,
	}
}

// WithKCPConfig 设置 KCP 传输参数。
//
// 只对 KCP 传输生效：其他传输没有 SetKCPConfig 方法，这里会跳过并打一条
// warn——静默忽略会让人以为配置生效了，实际仍在用默认参数。
func WithKCPConfig(c KCPConfig) ConnectOption {
	return func(s IConnectOption) {
		v, ok := s.(interface{ SetKCPConfig(KCPConfig) })
		if !ok {
			logger.Warnw(nil, "WithKCPConfig ignored: transport does not support kcp",
				"transport", fmt.Sprintf("%T", s))
			return
		}
		v.SetKCPConfig(c)
	}
}

// ResolveKCPConfig 从一组选项里取出 KCP 配置。
//
// 用途：底层监听器（MakeListener）在创建时还拿不到传输实例，
// 只能先把这组选项"试投"到一个只关心 KCP 配置的接收者上，取出配置。
// 与 WithKCPConfig 的判定规则同源，不会出现两处提取结果不一致。
func ResolveKCPConfig(opts []ConnectOption) KCPConfig {
	s := &kcpConfigSink{}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s.cfg
}

// kcpConfigSink 只接收 KCP 配置的选项接收者，其余方法空实现。
//
// ConnectOption 的形参是 IConnectOption，因此要把全部方法都实现出来
// 才能被选项调用；除 SetKCPConfig 外一律丢弃。
type kcpConfigSink struct{ cfg KCPConfig }

func (s *kcpConfigSink) SetKCPConfig(c KCPConfig) { s.cfg = c }

func (s *kcpConfigSink) SetCodec(c codec.Codec)                                      {}
func (s *kcpConfigSink) SetUriPath(path string) error                                { return nil }
func (s *kcpConfigSink) SetRouter(router *mux.Router) error                          { return nil }
func (s *kcpConfigSink) SetAddress(address string) error                             { return nil }
func (s *kcpConfigSink) SetServerHandleMessage(h handler.IServerHandleMessage) error { return nil }
func (s *kcpConfigSink) SetClientHandleMessage(h handler.IClientHandleMessage) error { return nil }
func (s *kcpConfigSink) SetHeader(key string, value string) error                    { return nil }
func (s *kcpConfigSink) SetOrigin(origins ...string) error                           { return nil }
