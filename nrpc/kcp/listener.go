package kcp

import (
	"fmt"
	"net"

	"github.com/w6xian/sloth/v4/option"

	kcpgo "github.com/xtaci/kcp-go/v5"
)

// MakeListener 在 UDP 地址上建 KCP 监听器。
//
// 本传输不需要 TLS：KCP 的保密性由 BlockCrypt 承担（见 option.KCPConfig.Crypt），
// 再套一层 TLS 只会多一次握手而没有额外收益。这与 QUIC 正相反——
// QUIC 没有证书就握不上手，必须对 tlsConf 报错，所以那里必须收这个参数。
//
// 加密方式与 FEC 参数必须在建监听器时就定下来：它们决定线上包的格式，
// 之后无法按连接调整。这也是本传输要实现 ListenerFactoryWithOptions 的原因
// （普通的 ListenerFactory 拿不到 Listen 传进来的选项）。
func MakeListener(address string, cfg option.KCPConfig) (net.Listener, error) {
	block, err := newBlockCrypt(cfg)
	if err != nil {
		return nil, err
	}
	ln, err := kcpgo.ListenWithOptions(address, block, cfg.DataShards, cfg.ParityShards)
	if err != nil {
		return nil, fmt.Errorf("kcp listen %s: %w", address, err)
	}
	return ln, nil
}

// Dial 拨一条 KCP 连接，并把调优参数应用上去。
//
// 两端必须使用相同的 crypt / key / FEC 配置：不一致时不会报"配置不匹配"，
// 而是解不开对端的包，表现为连接静默超时——排查时先核对两端配置。
func Dial(address string, cfg option.KCPConfig) (net.Conn, error) {
	block, err := newBlockCrypt(cfg)
	if err != nil {
		return nil, err
	}
	conn, err := kcpgo.DialWithOptions(address, block, cfg.DataShards, cfg.ParityShards)
	if err != nil {
		return nil, fmt.Errorf("kcp dial %s: %w", address, err)
	}
	tune(conn, cfg)
	return conn, nil
}
