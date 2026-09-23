# media：在 sloth 通道上跑 HTTP

一个**自包含**的最小示例：把内网一侧的目录暴露到外网，不新开端口、不定义新帧。

思路只有一句——**一次 RPC 承载一个完整的 HTTP 报文**：

```
浏览器 ──HTTP──▶ 网关（server）
                  ① 请求报文序列化（req.Write）
                  ② server.Call(ctx, userId, "http.Do", raw)
                                  ──────▶ 客户端（client）还原请求、打给本地 HTTP 服务
                                  ◀────── 响应报文（裸字节）
                  ③ 解析响应报文、回写给浏览器
浏览器 ◀──HTTP── 网关
```

于是 Range / 206 / Content-Type / MIME / 304 全部由客户端本地的 `http.FileServer`
提供，两端都只负责搬运，一行都不用自己实现。

尺寸上的两个前提（决定了这条路走得通）：

- 请求走**入参**，受 ag 限制单参数 ≤ 65535 字节 → 只放请求头，不放 body；
- 响应走**返回值**，是 fn 帧裸 payload，上限 1GB → 一个 206 分片绰绰有余。

## 跑起来

```bash
go run ./examples/media/server                 # RPC 8991 + 网关 8080
go run ./examples/media/client -root ./.media  # 连上来，把本地目录暴露成 /m/media/

curl -r 0-99 http://localhost:8080/m/media/sample.bin -D -
```

`server/main.go` 是网关侧（含 `v1.Sign` / `v1.Reg` 两个登记方法），
`client/main.go` 是内网侧（本地 FileServer + `http.Do` 转发服务）。

## 关于这份示例

它是为了把"隧道里跑 HTTP"这件事讲清楚而写的，**刻意保持自包含、不引外部库**，
所以网关逻辑直接写在示例里，没有做成可复用的包。

工程化的版本（网关、转发服务、鉴权中间件、分块与 Range、可注入日志、
"非主动不执行"的约束）已经独立成库，**更多相关应用请移步
<https://github.com/w6xian/sloth-proxy/tree/main/examples/media> 中查看。**
