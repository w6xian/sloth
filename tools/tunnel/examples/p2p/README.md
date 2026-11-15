# p2p coordinator（云端信令）

p2p 直连的**云端信令服务**：ws 登记/牵线 + UDP 地址观测 + work-id 管理。
**不转发任何业务流量**，打洞成功后退场。

> NAT 后节点的运行已内置进 sloth 框架（方案 A）：业务代码只写
> `Listen` + `WithCoordinate`/`WithWorkID`、`Dial("work-id@coord")`，
> 用法与部署见 [主模块示例](../../../../examples/p2p/README.md)，
> 原理与执行流见 [`doc/p2p.md`](../../../../doc/p2p.md)。
> 本目录只负责 coordinator 自身的部署与运维。

## 组成

| 目录 | 说明 |
|---|---|
| `coordinator/` | 信令服务（本 README 的主角） |
| `wire/` | P2P1 线上协议（REG/ACK/PUNCH 帧）与信令报文定义，与框架 p2p 引擎同约定 |
| `build.ps1` | 交叉编译（默认 linux/amd64，CentOS 可用） |
| `coordinator.service` | systemd 服务单元（开机自启 + 崩溃自动拉起） |

## ID 管理

| 操作 | 方式 |
|---|---|
| 生成 ID | `coordinator -create 备注 -store ids.json`（本地执行，打印 `work-<hex>` + token，**token 只显示一次**）；或 RPC `punch.CreateID` |
| 查看 ID 与在线状态 | RPC `punch.List`（台账 + 运行时出现过的匿名 id） |
| 接管凭据 | token sha256 入库，常数时间比较；台账内 id 必须携带正确 token，错误拒绝登记 |
| 匿名身份 | 空 id 自动分配一次性 `anon-<hex>`（客户端用，不占台账） |
| 离线判定 | ws 断开立即离线；REG 心跳 30s 超时兜底（peer 崩溃没走 OnClose 时） |
| 持久化 | `-store` 指定的 JSON 文件原子写，重启不丢；**不带 `-store` 时台账只在内存里，重启即丢** |

## 交叉编译与部署

```powershell
# Windows 上
./build.ps1                                    # => coordinator-linux-amd64
scp coordinator-linux-amd64 root@<云端IP>:/usr/local/bin/coordinator
scp coordinator.service      root@<云端IP>:/etc/systemd/system/
```

```bash
# 云端
sudo chmod +x /usr/local/bin/coordinator
sudo mkdir -p /var/lib/coordinator

# 生成正式 work-id（替代测试 id；生成后需重启 service 加载台账）
/usr/local/bin/coordinator -create 生产环境A -store /var/lib/coordinator/ids.json

sudo systemctl daemon-reload
sudo systemctl enable --now coordinator
```

注意 `coordinator.service` 的 `ExecStart` 要带 `-store /var/lib/coordinator/ids.json`，
否则 ID 台账不落盘。

常用运维命令：

```bash
systemctl status coordinator      # 状态
journalctl -u coordinator -f      # 跟日志
sudo systemctl restart coordinator
```

服务已带 `Restart=always`（崩溃 3s 后自动拉起）、开机自启、非 root
运行（端口 >1024 无需特权）；改监听端口就改
`/etc/systemd/system/coordinator.service` 里的 `ExecStart` 再
`daemon-reload` + `restart`。

## 端口与放行（CentOS）

coordinator 需要两个端口：**TCP 9100**（ws 信令）+ **UDP 9101**（地址观测）。
本机防火墙与云安全组**两处都要放行**，缺一不可。

```bash
# firewalld（CentOS 默认）
sudo firewall-cmd --permanent --add-port=9100/tcp
sudo firewall-cmd --permanent --add-port=9101/udp
sudo firewall-cmd --reload
sudo firewall-cmd --list-ports   # 应看到 9100/tcp 9101/udp
```

个别镜像 firewalld 未启用、走 iptables/nftables（`systemctl status firewalld`
显示 inactive 时先查 `iptables -L INPUT -n` 是否有 REJECT/DROP）：

```bash
sudo iptables -I INPUT -p tcp --dport 9100 -j ACCEPT
sudo iptables -I INPUT -p udp --dport 9101 -j ACCEPT
# 持久化：CentOS 7 装 iptables-services 后 service iptables save
```

云服务器多半还有一层**安全组**（阿里云/腾讯云/AWS 控制台），控制台里
也要放行 TCP 9100 和 UDP 9101。

外网验证端口连通（UDP 端无响应属正常——coordinator 只认 REG 报文，只要
框架 peer 能打印"观测到本端公网 UDP 地址"即说明通了）：

```bash
curl -v --max-time 5 http://<云端IP>:9100
echo "P2P1 REG test" | nc -u -w2 <云端IP> 9101
```

## 验证全链路

coordinator 跑通后，两端业务进程按主模块示例运行（见
[examples/p2p/README.md](../../../../examples/p2p/README.md)）：

1. 服务端（NAT 后）：`p2p -coord <云端IP>:9100 -id work-xxxx -token <token>`
   → coordinator 日志出现 `peer online peer=work-xxxx`；
2. 客户端（NAT 后）：`p2p-client -target work-xxxx@<云端IP>:9100`
   → 打洞 + QUIC 握手 + RPC 往返，此后 coordinator 日志**零数据流量**
   （数据全走直连）。

## 局限提醒

- 打洞成功率与 NAT 组合相关：锥型 ↔ 锥型可直连；任一侧 Symmetric NAT
  或禁 UDP 即失败（详见 doc/p2p.md 的"成功率与局限"）；
- coordinator 暴露公网，务必带 `-store` + token 鉴权运行，别用无鉴权的
  旧版本（旧版任何人都可用任意 `-id` 占坑）。
