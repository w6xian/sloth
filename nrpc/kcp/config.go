package kcp

import (
	"fmt"
	"net"
	"strings"

	"github.com/w6xian/sloth/v4/option"

	kcpgo "github.com/xtaci/kcp-go/v5"
)

// kcp-go 默认的收发窗口（包数）：窗口参数只给了一半时用它补齐另一半。
//
// SetWindowSize(0, 0) 会把窗口设成 0，连接直接卡死——所以不能把
// 未配置的那一半按 0 传进去。
const (
	defaultSndWnd = 32
	defaultRcvWnd = 32
)

// session 一条 KCP 连接上用于调优的方法集合。
//
// *kcpgo.UDPSession 满足它。抽成接口而不是直接断言具体类型，是为了让
// tune 可以用假连接测试，也为了让"拿到的是不是 KCP 连接"这件事显式化：
// 断言失败就跳过调优，而不是 panic。
type session interface {
	SetNoDelay(nodelay, interval, resend, nc int)
	SetWindowSize(sndwnd, rcvwnd int)
	SetMtu(mtu int) bool
	SetStreamMode(enable bool)
	SetACKNoDelay(nodelay bool)
}

// newBlockCrypt 按配置构造 KCP 的块加密器。
//
// Crypt 为空串时按 none 处理：KCP 可以不加密（加密与否只影响保密性，
// 不影响 KCP 本身的可靠性），要不要加密由部署环境决定。
func newBlockCrypt(cfg option.KCPConfig) (kcpgo.BlockCrypt, error) {
	key := cfg.Key
	switch strings.ToLower(strings.TrimSpace(cfg.Crypt)) {
	case "", option.KCPCryptNone:
		return kcpgo.NewNoneBlockCrypt(key)
	case option.KCPCryptXOR:
		return kcpgo.NewSimpleXORBlockCrypt(key)
	case option.KCPCryptTEA:
		return kcpgo.NewTEABlockCrypt(key)
	case option.KCPCryptAES:
		return kcpgo.NewAESBlockCrypt(key)
	case option.KCPCryptSalsa20:
		return kcpgo.NewSalsa20BlockCrypt(key)
	case option.KCPCryptBlowfish:
		return kcpgo.NewBlowfishBlockCrypt(key)
	case option.KCPCryptSM4:
		return kcpgo.NewSM4BlockCrypt(key)
	default:
		return nil, fmt.Errorf("kcp: unknown crypt %q", cfg.Crypt)
	}
}

// tune 把调优参数应用到一条 KCP 连接。
//
// 零值字段一律不调用对应的 Set，保持 kcp-go 的默认行为（见 KCPConfig 注释）。
// 不是 KCP 连接（断言失败）时直接返回，不做任何事。
func tune(conn net.Conn, cfg option.KCPConfig) {
	s, ok := conn.(session)
	if !ok || s == nil {
		return
	}
	// interval 必须一起给：只开 nodelay 而 interval=0 会让 KCP 每收到一个包
	// 就 flush 一次，CPU 空转（见 KCPConfig.NoDelay 注释）。
	if cfg.NoDelay > 0 && cfg.Interval > 0 {
		s.SetNoDelay(cfg.NoDelay, cfg.Interval, cfg.Resend, cfg.NC)
	}
	if cfg.SndWnd > 0 || cfg.RcvWnd > 0 {
		snd, rcv := cfg.SndWnd, cfg.RcvWnd
		if snd <= 0 {
			snd = defaultSndWnd
		}
		if rcv <= 0 {
			rcv = defaultRcvWnd
		}
		s.SetWindowSize(snd, rcv)
	}
	if cfg.MTU > 0 {
		s.SetMtu(cfg.MTU)
	}
	if cfg.StreamMode {
		s.SetStreamMode(true)
	}
	if cfg.ACKNoDelay {
		s.SetACKNoDelay(true)
	}
}
