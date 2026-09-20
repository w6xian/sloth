// Package errs 定义 sloth 全库共用的哨兵错误（sentinel error）。
//
// 目的：调用方需要能**判定**错误类型来决定后续行为——超时可重试、连接已关要重连、
// 队列满要退避、对端没注册方法是配置问题重试无意义。而 errors.New("call timeout")
// 这种裸字符串错误只能靠文本匹配判定，文案一改就全崩。
//
// 约定：所有产生点用 fmt.Errorf("%w: 上下文", X) 包裹，调用方统一用
// errors.Is(err, sloth.ErrTimeout) 判定（根包 sloth 已转发这些变量）。
package errs

import "errors"

var (
	// ErrTimeout 等待对端响应超时。可重试，但要确认方法幂等。
	ErrTimeout = errors.New("sloth: rpc timeout")

	// ErrConnClosed 连接已关闭或不可用。需重连后再重试。
	ErrConnClosed = errors.New("sloth: connection closed")

	// ErrQueueFull 待发/回包队列已满（背压）。应退避或限流，立即重试只会加剧拥塞。
	ErrQueueFull = errors.New("sloth: rpc queue full")

	// ErrNoChannel 目标用户/房间当前没有可用连接（对端离线）。重试无意义。
	ErrNoChannel = errors.New("sloth: channel not found")

	// ErrNotServing 本进程尚未建立可用通道：服务端未 Listen，或客户端未 Dial 成功。
	ErrNotServing = errors.New("sloth: server not started")

	// ErrMethodNotFound 对端未注册该方法。属于配置/版本问题，重试无意义。
	ErrMethodNotFound = errors.New("sloth: method not found")
)
