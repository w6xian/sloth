package main

// agent 的配置。
//
// 一条隧道 = relay 上同 id 的隧道；target 是 agent 所在内网里
// 真正要拨的本地服务地址。token 必须与 relay 配置一致。

// TunnelCfg 单条隧道的配置。
type TunnelCfg struct {
	ID     string `toml:"id"`     // 隧道 ID，与 relay 配置同名
	Token  string `toml:"token"`  // 接管凭据，与 relay 配置一致
	Target string `toml:"target"` // 本地目标，如 127.0.0.1:8080
	Mode   string `toml:"mode"`   // tcp（默认）| udp：决定 OPEN 时怎么拨 target，需与 relay 一致
}

// Config agent 整体配置。
type Config struct {
	Relay   string      `toml:"relay"` // relay 的 ws 地址，如 1.2.3.4:9100
	Name    string      `toml:"name"`  // agent 名，仅用于日志辨识
	Tunnels []TunnelCfg `toml:"tunnel"`
}
