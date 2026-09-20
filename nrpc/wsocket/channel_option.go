package wsocket

// defaultChannelQueueSize channel 各队列（待发 RPC、回包、广播）的默认容量。
//
// 容量决定了"对端/本端处理不过来时能缓冲多少"，是背压的第一道闸门：
// 太小→突发时频繁报队列满；太大→内存占用高且延迟被掩盖。
// 默认 10 是低延迟优先的取值，推送量大或 RTT 高的场景应调大。
const defaultChannelQueueSize = 10

type ChannelServerOption func(ch *WsChannelServer)
type ChannelClientOption func(s *WsChannelClient)

// WithServerQueueSize 设置服务端 channel 的各队列容量（待发 RPC / 回包 / 广播）。
// n <= 0 表示用默认值 10。
func WithServerQueueSize(n int) ChannelServerOption {
	return func(ch *WsChannelServer) {
		if n > 0 {
			ch.queueSize = n
		}
	}
}

// WithClientQueueSize 设置客户端 channel 的各队列容量。n <= 0 表示用默认值 10。
func WithClientQueueSize(n int) ChannelClientOption {
	return func(c *WsChannelClient) {
		if n > 0 {
			c.queueSize = n
		}
	}
}

// WithServerQueueFullHook 注册"队列满"回调（用于打点观测背压）。
// 每次因队列满而丢弃一次投递时调用一次。
func WithServerQueueFullHook(hook func()) ChannelServerOption {
	return func(ch *WsChannelServer) {
		ch.onQueueFull = hook
	}
}

// WithClientQueueFullHook 客户端侧的队列满回调，语义同 WithServerQueueFullHook。
func WithClientQueueFullHook(hook func()) ChannelClientOption {
	return func(c *WsChannelClient) {
		c.onQueueFull = hook
	}
}
