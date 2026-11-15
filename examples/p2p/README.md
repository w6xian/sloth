# p2p 示例（框架内置 NAT 打洞 + QUIC）

`sloth` 框架内置 p2p：服务端 `Listen` 带 `WithCoordinate` + `WithWorkID` 时
不再直接监听，而是先经 coordinator 牵线打洞，把 QUIC 跑在打洞的那条
UDP socket 上；客户端 `Dial("对端ID@coordinator地址")` 即自动走 p2p。
**对上层完全同构**：业务代码与直连云端一模一样，只是地址语法不同。

原理与完整执行流见 [`doc/p2p.md`](../../doc/p2p.md)。

## 角色与拓扑

```
服务端（家里/公司，NAT 后）      coordinator（云端固定 IP）       客户端（NAT 后）
  examples/p2p  (main.go)        tools/tunnel/examples/p2p/      examples/p2p/client
  work-id + token                coordinator（ws:9100/udp:9101）  Dial "work-id@coord:9100"
```

- coordinator 只做信令（牵线 + 地址观测 + ID 管理），**不转发业务流量**；
- 打洞成功后 coordinator 退场，数据在两端直连 UDP（QUIC）上流动。

## 一、部署 coordinator（云端，CentOS）

程序与部署细节在 [`tools/tunnel/examples/p2p/`](../../tools/tunnel/examples/p2p/README.md)，
这里是要点：

```powershell
# Windows 上交叉编译（在 tools/tunnel/examples/p2p 下）
./build.ps1                       # => coordinator-linux-amd64
```

```bash
# 云端：生成一个 work-id（token 明文只显示一次）
./coordinator-linux-amd64 -create 生产环境A -store /var/lib/coordinator/ids.json
#   id:    work-1a562d15
#   token: 4ef05e20...（妥善保管）

# 启动（建议 systemd 服务，见 tools/tunnel/examples/p2p/coordinator.service）
./coordinator-linux-amd64 -rpc :9100 -udp :9101 -store /var/lib/coordinator/ids.json
```

云端放行端口：**TCP 9100**（ws 信令）+ **UDP 9101**（地址观测），
本机防火墙与云安全组两处都要放（CentOS firewalld/iptables 命令、
安全组、验证方法见 [tools/tunnel/examples/p2p/README.md](../../tools/tunnel/examples/p2p/README.md) 的
"云端端口放行（CentOS）"一节）。

ID 管理：

| 操作 | 方式 |
|---|---|
| 生成 ID | `coordinator -create 备注`（本地，打印 id+token 后退出）；或 RPC `punch.CreateID` |
| 查看 ID 与在线状态 | RPC `punch.List`（台账内 + 运行时出现过的匿名 id） |
| 接管凭据 | token sha256 入库，常数时间比较；台账内 id 必须携带正确 token，错误拒绝登记 |
| 离线判定 | ws 断开立即离线；REG 心跳 30s 超时兜底（peer 崩溃没走 OnClose 时） |
| 持久化 | `ids.json` 原子写，重启不丢 |

## 二、部署服务端（被连端，NAT 后的机器）

```bash
# 本目录构建（或直接 go run .）
go build .
```

```bash
./p2p \
  -coord <云端IP>:9100 \
  -id work-1a562d15 \
  -token 4ef05e20...            # 台账里的 id 必须带 token
```

启动参数：

| 参数 | 默认 | 说明 |
|---|---|---|
| `-coord` | `coord.graybook.com.cn:9100` | coordinator 的 ws 信令地址 |
| `-id` | `work-id-abde` | 本端 work-id（coordinator `-create` 生成） |
| `-token` | 空 | work-id 的接管凭据；台账内 id 必填，缺失/错误会被 coordinator 拒绝 |

启动成功的标志（日志）：

```
INF msg="peer online" peer=work-1a562d15          # coordinator 侧
INF msg="udp addr observed" addr=<公网IP>:8992     # coordinator 侧
INF msg="p2p ready" work_id=... observed=...      # 本端
INF msg="starting quic server on localhost:8992"
```

说明：

- 监听地址 `0.0.0.0:8992` 是 UDP socket 的 **bind 地址**；这条 socket 会
  一路用到底（REG 心跳 → 探针 → QUIC 监听），**不能**再另开端口监听
  （NAT 映射与 socket 绑定）。**必须绑 0.0.0.0 或内网 IP**——绑
  `127.0.0.1` 的 socket 出不了公网，REG/探针发不到 coordinator
  （仅单机三端联调可用 127.0.0.1）；
- UDP 坐标（`-coord` 端口+1）只是提示值，coordinator 在 Register 响应里
  会带回准确地址，框架优先用它；
- QUIC 强制 TLS：示例运行时生成自签证书（24h 有效），生产请换正式证书。

## 三、部署客户端（主动端，NAT 后的机器）

```bash
go build ./client
```

```bash
./client \
  -target work-1a562d15@<云端IP>:9100
```

`-target` 语法：`对端ID@coordinator地址`。含 `@` 即走 p2p（先打洞再 QUIC
握手）；不含 `@` 则与普通直连完全相同——**Dial 语义不变，业务代码无感知**。

客户端不填 work-id：coordinator 自动分配一次性匿名 id（`anon-<hex>`），
仅本次牵线有效。

成功标志（日志）：

```
INF msg="p2p punched through" target=work-1a562d15 remote=<服务端公网IP>:8992 id=anon-xxxx
INF msg="sign success" userId=2
INF msg="v1.Test result" data="..." err=null     # 每 2s 一次 RPC 往返
```

## 四、本地联调（单机三端）

打洞在本机必然成功，用于验证代码路径：

```bash
# 云端 coordinator（tools/tunnel/examples/p2p 下）
coordinator -rpc 127.0.0.1:9100 -udp 127.0.0.1:9101

# 服务端
./p2p -coord 127.0.0.1:9100 -id test-id

# 客户端
./client -target test-id@127.0.0.1:9100
```

验证点：客户端 `p2p punched through` 后 `v1.Test` 持续返回，且 coordinator
日志在 `peers introduced` 之后**零数据流量**（数据全走直连）。

## 五、注意事项与限制

| 项 | 说明 |
|---|---|
| NAT 类型 | 锥型 NAT 可直连；任一侧 Symmetric NAT（手机热点、部分公司网）打洞失败，`Dial` 返回 `p2p.ErrPunchFailed`（生产应回落云端中继） |
| UDP 封锁 | 任一侧禁 UDP 出站/入站则失败 |
| token 安全 | 台账内 id 必须带 token；token 丢失只能重新 `-create`（旧 id 可删台账文件后重建） |
| TLS | 示例自签 + 客户端 `InsecureSkipVerify`，等于放弃身份校验；生产用正式证书，或让 coordinator 作信任锚校验证书指纹 |
| 重连 | ws 信令断线自动重连并重新登记；QUIC 断线自动重连且复用打洞的同一条 socket |
| 端口 | 服务端机器**不需要**对外开端口（出站打洞）；只有 coordinator 需要放行 TCP 9100 + UDP 9101 |
