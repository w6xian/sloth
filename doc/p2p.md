# sloth p2p 直连：原理与执行流

框架内置的 NAT 打洞能力（方案 A）：业务侧永远是 `Listen` / `Dial` / `Call`，
地址写成 `对端work-id@coordinator地址` 即走 p2p——框架内部先经 coordinator
牵线打洞，再把 QUIC 跑在打洞的那条 UDP socket 上。信令面建在 sloth ws 上
（复用 tunnel 已踩平的 JSON 信封 / 恒等 codec / tlv.Json 坑位），数据面
QUIC（quic-go），coordinator 只做信令、**不转发任何业务流量**。

## 一、三端角色

```
┌─────────────┐         ┌──────────────────┐         ┌─────────────┐
│  peer 服务端 │         │   coordinator     │         │  peer 客户端 │
│ (被连端)     │         │  云端固定公网 IP    │         │ (主动端)     │
│ work-id     │         │  TCP:9100 ws 信令  │         │ 匿名 id      │
│ NAT 后      │         │  UDP:9101 地址观测 │         │ NAT 后       │
└─────────────┘         └──────────────────┘         └─────────────┘
```

| 端 | 程序 | 职责 |
|---|---|---|
| coordinator | `tools/tunnel/examples/p2p/coordinator` | 信令：`punch.CreateID / Register / Connect / List` + UDP REG 地址观测；ID 台账（token 鉴权）持久化在 `ids.json` |
| 服务端 peer | 业务进程，`Connect.Listen` | 登记 work-id，等 client 牵线 |
| 客户端 peer | 业务进程，`Connect.Dial` | 匿名登记，请求与目标 work-id 牵线并打洞 |

## 二、核心原理：为什么必须复用同一条 socket

NAT 的出站映射绑定在 `(内网 ip:port → 公网 ip:port)` 这条流上：

- peer 用**同一个 UDP socket** 既向 coordinator 发 REG 心跳，又对对端发
  PUNCH 探针；
- cone 类 NAT（Full-cone / 受限锥型，家庭宽带大多属于此类）对同一 socket
  的出站映射稳定 → coordinator 观测到的地址 = 对端可直达地址；
- **换了 socket = 换了映射 = 对方永远进不来** → 打洞的那条 socket 最后直接
  交给 QUIC（`quic.ListenPacketConn` / `quicgo.Dial(ownConn, remote)`），
  不再新建端口监听；
- Symmetric NAT（手机热点、部分公司网）每个目的地址映射都不同 → 观测地址
  失效 → 打不通（超时 20s，上层应回落云端中继，即 tunnel v1 的 relay/agent）。

## 三、执行流（时序图）

```
 服务端(Listen)                coordinator                     客户端(Dial "id@coord")
────────────────────────────────────────────────────────────────────────────────────
 ① bind UDP :8992                                            ② bind UDP 随机端口
      │                                                            │
      │──── ws 拨入 punch.Register {id, token} ──▶│               │──── ws 拨入 punch.Register {空 id} ──▶│
      │◀─── regResp {ok, id, coord_udp} ─────────│               │◀─── regResp {ok, id=anon-x, coord_udp} │
      │    （token 常数时间校验；重试到成功）          │               │                                        │
      │                                                            │
 ③ REG 心跳（UDP，每 2s）─────▶│ 观测服务端公网地址      ③' REG ──────▶│ 观测客户端公网地址
      │◀─── ACK("你 observed=ip:port") ──│                       │◀─── ACK ──│
      │    （Establish 独占读窗口到此结束）                       │
      │                                                            │
 ④ 后台：Keepalive（只写不读）+ WatchIntros（等 Intro）            │
      │                                       │◀── punch.Connect {from:anon-x, to:work-id} ──│
      │                                       │ 校验双方在线+地址已观测          │
      │◀───── Intro {session, peer_addr=客户端地址} ───────│──── connectResp {session, peer_addr=服务端地址} ──▶│
      │                                       │（coordinator 至此退场）      │
      │                                                        │
 ⑤ 探针突发：对 peer_addr 狂发 PUNCH（150ms/次 × 15s）          ⑤' PunchOut：对 peer_addr 发 PUNCH + 读
      │  （纯写，不与 QUIC 抢 socket）                          │     直到收到对方的 PUNCH → 确认洞已凿开
      │◀══════════════ PUNCH 探针互发（同一条 socket）═════════════════════▶│
      │                                                        │
 ⑥ quic.ListenPacketConn(ownSock, tls)                        ⑥' quicgo.Dial(ownSock, remote, tls)
      │◀══════════════ QUIC 握手（Initial 走已打通的洞）══════════════════▶│
      │                                                        │
 ⑦ 注册业务服务（v1.Test…）                          ⑦' client.Call / ch.Push —— 上层 API 与云端直连完全同构
```

## 四、状态机 / 流程图

```
            【服务端路径：Connect.Listen + WithCoordinate + WithWorkID】
   Listen(ctx, QUIC, ":8992", opts...)
        │
        ▼
   解析 opts：有 Coordinate？ ──否──▶ 普通监听（net.Listen / MakeListener）
        │是
        ▼
   punchServeSocket：
   bind UDP socket ──▶ 拨 coordinator ws ──▶ Register(id, token)
        │                                     │ 失败重试（1s×30）
        ▼◀────────────────────────────────────┘
   Establish：REG → 等 ACK → 拿观测地址（独占读）
        │
        ├─▶ Keepalive goroutine：REG 每 2s（只写）
        ├─▶ WatchIntros goroutine：收 Intro → 对新对端 probeBurst（只写）
        ▼
   PacketConnListenerFactory：quic.ListenPacketConn(这条 socket)   ← socket 读权移交 QUIC
        │
        ▼
   Serve()：等 QUIC 连接进来 → 正常 RPC

            【客户端路径：Dial("work-id@coord:9100")】
   地址含 "@"？ ──否──▶ 普通直连 DialAddr
        │是（target=work-id, coord=addr）
        ▼
   punchDialSocket：
   bind UDP 随机端口 ──▶ 拨 coordinator ws ──▶ Register(空 id → 匿名)
        ▼
   Establish：REG → ACK（独占读）
        ▼
   punch.Connect(from, target) ──▶ 收 {session, peer_addr}
        ▼
   PunchOut：发 PUNCH + 读，直到收到对端探针（独占读，超时 20s）
        │                    └─ 超时 → ErrPunchFailed（上层可回落中继）
        ▼
   SetPunchedConn(sock, remote) 注入 QUIC 客户端
        ▼
   quicgo.Dial(ownSock, remote)   ← socket 读权移交 QUIC
        ▼
   正常 RPC（Call / Push / handler），断线重连也复用同一条 socket
```

## 五、UDP 线上协议（P2P1 帧）

数据面是同一条 socket 上的三段式文本帧（coordinator 与两端共用）：

| 帧 | 方向 | 用途 |
|---|---|---|
| `P2P1 REG <id>` | peer → coordinator | 登记/保活本端 UDP 地址（每 2s） |
| `P2P1 ACK <addr>` | coordinator → peer | 回观测到的公网地址 |
| `P2P1 PUNCH <session> <seq>` | 双方互发 | 打洞探针（150ms/次），先收到即确认直连 |

信令面（ws）报文：`punch.Register / Connect / CreateID / List`，TLV 载荷
`tlv.Json` 编码；Intro（引见报文）经 ws 推送，JSON 信封
`{"type":1,"body":"<base64>"}`，handler.OnData 解封后解析。

## 六、关键设计点（容易踩错的）

| # | 设计点 | 原因 |
|---|---|---|
| 1 | **读窗口独占**：Establish/PunchOut 阶段读 socket，结束后才让 QUIC Transport 接管 | Transport 接管后独占读走全部数据报；p2p 之后只**写**不读（REG/PUNCH 纯写，回包无人读无碍） |
| 2 | **打洞确认以实际来源地址为准**（先收到探针的 `from`），不用宣告地址 | 双侧 NAT 观测可能不对称 |
| 3 | 服务端收到 Intro 只做**探针突发 15s**，不阻塞 | 为对端 QUIC 握手包凿开本端入站许可 |
| 4 | ws 断线自动重连 → OnReady 自动重新 Register | 信令面持久化；直连是独立 UDP 不受影响 |
| 5 | token 常数时间比较（sha256 入库，`-create` 生成、明文只显示一次） | 防冒名占坑；台账 `ids.json` 原子持久化，重启不丢 |
| 6 | 空 id 自动分配匿名 `anon-<hex>`（客户端），台账外非空 id 兼容放行 | 客户端无需申请即可临时身份 |
| 7 | 业务无感知：上层永远是 `Listen`/`Dial`/`Call` | `work-id@coord` 只是地址语法，打不通未来可静默回落中继 |

## 七、业务侧用法

```go
// 服务端：被连端（work-id 由 coordinator -create 生成）
drpc.Listen(ctx, sloth.QUIC, "localhost:8992",
    option.WithCoordinate(sloth.Coordinator(sloth.WS, "coord.example.com:9100")),
    option.WithCoordinate(sloth.Coordinator(sloth.UDP, "coord.example.com:9101")),
    option.WithWorkID("work-id-abde", token), // 台账内的 id 必须带 token
    option.WithTcpHandleMessage(&Handler{}))

// 客户端：主动端（匿名登记）
newConnect.Dial(ctx, sloth.QUIC, "work-id-abde@coord.example.com:9100")
```

客户端证书需 `InsecureSkipVerify`（两端都是 NAT 后节点，无正式证书）；
生产加固可让 coordinator 作信任锚——登记时交换证书指纹，Dial 时校验对端
证书指纹与 coordinator 记录一致（Tailscale 的做法）。

## 八、成功率与局限

| NAT 组合 | 结果 |
|---|---|
| 锥型 ↔ 锥型（家庭宽带大多属于此类） | ✅ 可打洞直连 |
| 任一侧 Symmetric NAT（手机热点、部分公司网） | ❌ 失败（每个目的地址映射不同） |
| 任一侧防火墙禁 UDP | ❌ 失败 |

- 打不通时 `Dial` 返回 `p2p.ErrPunchFailed`；生产实现应回落云端中继
  （`tools/tunnel` v1 的 relay/agent），业务代码一行不改。
- TLS 用自签证书 + `InsecureSkipVerify` 等于放弃校验，生产建议固定公钥或
  coordinator 指纹校验（见上节）。
- coordinator 安全：只放行 TCP 9100 + UDP 9101，wss 可挂 nginx/Caddy 终结
  TLS；部署为 systemd 服务见 `tools/tunnel/examples/p2p/README.md`。

## 九、验证记录（本地三端，2026-11-15）

```
coordinator: peer online work-1a562d15 → anon-7cec4efb → peers introduced session=1
client:      p2p punched through remote=127.0.0.1:8992 id=anon-7cec4efb
             sign success → v1.Test result 循环调用成功
coordinator: 之后零数据流量（全部直连）
```

关键佐证：客户端直连地址 8992 = 服务端监听端口，证明 QUIC 握手确实复用了
打洞的那条 socket，没有新建端口。
