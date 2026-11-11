package main

// relay 的配置。
//
// 一条隧道 = 一个 tunnel id + token（agent 侧同名同 token 才能接管）。
// mode 决定公网入口形态：
//   - tcp：独立监听一个端口，访客连 listen，流量整体透传；
//   - http：多个 http 隧道可共用同一个 listen 端口，按请求 Host 头分发；
//   - udp：独立监听一个 UDP 端口，按访客五元组建会话透传数据报。

// TunnelCfg 单条隧道的配置。
type TunnelCfg struct {
	ID     string `toml:"id"`     // 隧道 ID，agent 配置同名
	Token  string `toml:"token"`  // 接管凭据，agent 必须携带相同值
	Mode   string `toml:"mode"`   // tcp | http | udp
	Listen string `toml:"listen"` // 公网监听地址（http 模式多条隧道可共用）
	Host   string `toml:"host"`   // http 模式：匹配的 Host 头，支持 *.example.com 通配
}

// Config relay 整体配置。
type Config struct {
	RPCAddr string      `toml:"rpc_addr"` // agent 拨入的 ws 监听地址
	Tunnels []TunnelCfg `toml:"tunnel"`
}
