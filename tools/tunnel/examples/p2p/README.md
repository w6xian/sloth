# p2p 打洞实例（三端）

演示"两个 NAT 后节点经云端信令牵线后 UDP 直连"的完整流程。
这是 `tools/tunnel` 二期（打洞直连）的**技术预研实例**：信令面直接建在
sloth 上（复用 tunnel 已踩平的 JSON 信封 / 恒等 codec 坑位），数据面用
裸 UDP 文本协议（生产应换 KCP/QUIC）。

## 三端

| 端 | 程序 | 位置 | 职责 |
|---|---|---|---|
| coordinator | `coordinator/` | 云端固定 IP | 信令：ws 登记/牵线 + UDP 观测各端公网地址。**不转发任何业务流量** |
| peer A | `peer/` | 家里（NAT 后） | 被连端：`-id home` |
| peer B | `peer/` | 公司（NAT 后） | 主动端：`-id office -connect home` |

## 流程

```
peer A                coordinator                peer B
  │ ws 注册             │                           │ ws 注册
  │ REG(UDP, 每2s) ────▶│ 观测 A 公网地址            │ REG(UDP, 每2s) ────▶│
  │◀───── ACK(观测地址) │                           │◀───── ACK(观测地址) │
  │                    │◀── punch.Connect(A→B) ─────│
  │◀── Intro(B地址) ───│──── Intro(A地址) ─────────▶│
  │        PUNCH 探针 ◀────────── UDP ──────────▶ PUNCH 探针（双方互发）
  │              ★ 直连建立，coordinator 退场
  │◀══════════════ DATA（直连 UDP 聊天）════════▶│
```

打洞为什么能成：peer 用**同一个 UDP socket** 既跟 coordinator 保持 REG
心跳、又对对方公网地址发探针——cone 类 NAT（Full-cone / 受限锥型）对同一
socket 的出站映射稳定，coordinator 观测到的地址即对端可直达地址；两侧同时
互发探针，任一侧先收到即确认洞已凿开。

## 运行

```bash
# 云端（固定 IP）
go build -o coordinator.exe ./coordinator
./coordinator.exe -rpc :9100 -udp :9101

# 家里
go build -o peer.exe ./peer
./peer.exe -server ws://<云端IP>:9100 -coord <云端IP>:9101 -id home

# 公司（主动连家里）
./peer.exe -server ws://<云端IP>:9100 -coord <云端IP>:9101 -id office -connect home
```

连上后任意一端敲回车即可互发消息；`★ P2P 直连建立` 一行会打印两端的
实际 UDP 地址，可对照确认流量没有经过云端。

### 本地联调（同一台机器跑三端）

打洞在本机必然成功，用于验证代码路径：

```bash
./coordinator.exe
./peer.exe -id home
./peer.exe -id office -connect home
```

预期输出（两端都有）：

```
[home] 已向 coordinator 登记
[home] coordinator 观测到本端公网 UDP 地址: 127.0.0.1:5xxxx
[home] coordinator 牵线成功: 对端 office @ 127.0.0.1:5yyyy
[home] ★ P2P 直连建立: 本端 127.0.0.1:5xxxx <-> 对端 127.0.0.1:5yyyy（流量不再经过 coordinator）
[home] << hello from office (via direct UDP)
```

## 成功率与局限（务必清楚）

| NAT 组合 | 结果 |
|---|---|
| 锥型 ↔ 锥型（家庭宽带大多属于此类） | ✅ 可打洞直连 |
| 任一侧 Symmetric NAT（手机热点、部分公司网） | ❌ 失败（每个目的地址映射不同，观测地址失效） |
| 任一侧防火墙禁 UDP | ❌ 失败 |

失败时 peer 会在 20s 超时并打印原因与建议——生产实现的正确做法是**回落
云端中继**：`tools/tunnel` v1（relay/agent）就是现成的中继通道，二期把
"打洞失败 → 自动回落中继"串起来即可。

其他局限（实例刻意从简）：

- 数据面是 UDP 文本协议，无加密、无重传、无拥塞控制——生产请换 QUIC
  （sloth 已带 quic-go 依赖）或 KCP；
- 探针/心跳间隔写死（150ms / 2s），未做退避与自适应；
- 打洞成功后的会话没有保活探测，NAT 映射过期后需重新牵线；
- `-id` 无鉴权，谁都能冒名登记——生产要加 token（参考 tunnel 的
  常数时间比较）。
