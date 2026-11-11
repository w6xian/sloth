# tunnel — 基于 sloth 的内网穿透隧道

内网穿透工具：家里任意网络里的服务，通过云端固定 IP 暴露给公网访问。
全部复用 sloth 的长连接能力（ws 拨入、推送通道做数据面、框架自带断线重连），
**不需要在访问方安装任何东西**——浏览器 / curl 直连云端口即可。

## 架构

```
                    云端固定 IP
              ┌──────────────────────┐
              │ relay                │
              │  · ws :9100 (agent)  │   ← agent 主动拨入（出站，NAT 无关）
              │  · 公网端口 (访客)    │
              └──────┬───────────────┘
               ws 拨入│        │ 公网端口
   ┌────────────────┴──┐  ┌──┴──────────┐
   │ agent（家里常开机） │  │ 访客（公司）  │
   │  · 断线自动重连     │  │ 无需装任何东西│
   └────────┬──────────┘  └─────────────┘
            │ 局域网
       file-server（127.0.0.1:8080）
```

- **agent → relay**：出站 ws，任何 NAT/公司网都通；断线由 sloth 自动重连，
  重连后自动重新登记（Register 校验 id + token）。
- **数据面**：fire-and-forget 推送（relay 用 `ch.Push`，agent 用 `client.Send`），
  每条 ws 消息承载一个隧道帧（`proto` 包，32KB 分块，magic 0xC7 避开 FN 帧），
  同一条连接内顺序天然保序。
  两个框架行为必须知道（详见 `proto/envelope.go`）：ws 数据面的推送在两端
  `OnData` 收到的是 `message.Msg` 的 JSON 信封；而 `client.Send` 默认还会先套
  一层 ag 帧，relay 的 `tlvValue` 兜底会把它误当 TLV 帧撕头截断——因此 agent
  把 `SetEncoder/SetDecoder` 设为恒等，让 0xC7 帧原样上线（0xC7 打头使 TLV 的
  4 字节长度字段必然越界，反而获得原样透传）。
- **隧道模式**：
  - `tcp`：一个公网端口整体透传给一条隧道（适合任意 TCP 服务）；
  - `http`：多条隧道共用一个公网端口，按请求 `Host` 头分发
    （支持 `*.example.com` 通配；HTTPS/SNI 透传 v1 不支持，见下）；
  - `udp`：一个公网 UDP 端口透传给一条隧道。UDP 无连接，relay 按
    访客五元组（ip:port）建会话：首个数据报分配 connID 并推 OPEN，
    之后每报一帧 DATA；agent 侧为每个会话拨一条"已连接"的本地 UDP
    socket，报文边界一对一保留。会话空闲 60s 自动回收（回 CLOSE 让
    agent 释放本地 socket）；agent 断线时全部会话立即作废，访客下一个
    数据报自动重建——丢包语义与 UDP 本身一致。适合 DNS 转发、游戏、
    监控上报等 UDP 服务。

## 构建

`tools/tunnel` 是独立子模块（`replace` 指回 `../..`）：

```bash
cd tools/tunnel
go build -o relay.exe ./relay
go build -o agent.exe ./agent
```

## 配置

示例见 [conf/relay.example.toml](conf/relay.example.toml) 与
[conf/agent.example.toml](conf/agent.example.toml)。

要点：

- 一条隧道 = `id` + `token`；agent 配置同 id 同 token 才能接管，token 不匹配
  直接拒绝（常数时间比较）。target 只留在 agent 本地，不出内网。
- relay 的 `listen` 支持 `0.0.0.0`；http 模式多条隧道可共用同一 `listen`。
- agent 支持同时挂多条隧道；也可以多个 agent 各挂不同隧道（id 不能冲突）。

## 验证（单机模拟全链路）

```bash
# 1. 内网"file-server"（任意本地 HTTP 服务都行）
go run ./dev/target

# 2. relay
go run ./relay -conf conf/dev-relay.toml

# 3. agent
go run ./agent -conf conf/dev-agent.toml

# 4. 访客
curl http://127.0.0.1:18080/                        # tcp 隧道
curl -H "Host: files.local" http://127.0.0.1:18081/ # http 隧道

# 5. udp 隧道（dev/target 自带 UDP echo，回大写应答）
#    PowerShell：
$u = New-Object Net.Sockets.UdpClient; $u.Connect('127.0.0.1',18082)
$b=[Text.Encoding]::UTF8.GetBytes('ping'); [void]$u.Send($b,$b.Length)
$ep=New-Object Net.IPEndPoint([Net.IPAddress]::Any,0)
[Text.Encoding]::UTF8.GetString($u.Receive([ref]$ep))   # => PING
```

## 已知限制（v1）

- **无背压**：数据面是 fire-and-forget 推送，agent/relay 消费慢时内存会涨
  （ws 写缓冲）。file-server 这类低带宽场景无感；高吞吐场景需要二期加流控。
- **不支持 HTTPS/SNI 透传**：http 隧道收到 TLS 握手直接拒绝。要上 HTTPS：
  在 relay 前面挂 nginx/Caddy 终结 TLS 反代到 http 隧道端口，或二期加 SNI 路由。
- **流量全走云端中继**：吃云带宽。打洞直连（两端交换公网映射后走 KCP/QUIC，
  云只留信令）是规划中的二期优化，sloth 的多协议能力已备好。
- **token 是静态凭据**：泄露即被冒用。二期可加一次性登记票据。
- agent 断线期间访客连接立即失败（连接被复位），客户端重试即可，
  无排队/等待语义。
- **udp 隧道限制**：单报 ≤ 32KB（超限丢弃，不做拆分以保报文边界）；
  本地服务回包 > 32KB 会话会被 agent 关闭；会话空闲 60s 回收，
  超长空闲的请求方需重发。

## 二期路线

1. 打洞直连：relay 做信令，两端交换公网映射（复用 sloth 的 KCP/QUIC 传输），
   失败回落现有中继链路。技术预研实例已就位：
   [`examples/p2p`](examples/p2p/README.md)（coordinator 信令 + UDP 地址观测 +
   双端 PUNCH 探针，本地可跑通全流程）。
2. TLS/SNI 透传：http 隧道按 ClientHello 的 SNI 分发，字节原样透传。
3. 流控背压：按 connID 做简单信用窗口。
