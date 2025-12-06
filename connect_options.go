package sloth

import "time"

// connect options

type IRpcOption func(IRpc)
type ServerRpcOption func(*ServerRpc)
type ClientRpcOption func(*ClientRpc)

// 链接相关参数，透传到ws中
type ConnOption func(*Connect)

func WithSleepTimes(times int) ConnOption {
	return func(c *Connect) {
		c.sleepTimes = times
	}
}

func WithTimes(times int) ConnOption {
	return func(c *Connect) {
		c.times = times
	}
}

func WithCpuNum(cpuNum int) ConnOption {
	return func(c *Connect) {
		c.cpuNum = cpuNum
	}
}

func WithClientLogic(l *ClientRpc) ConnOption {
	return func(c *Connect) {
		c.client = l
	}
}
func WithServerLogic(l *ServerRpc) ConnOption {
	return func(c *Connect) {
		c.server = l
	}
}

func WithPongTimeout(timeout time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.PongWait = timeout
	}
}

// readWait default eq 10s
func WithReadWait(readWait time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.ReadWait = readWait
	}
}

func WithWriteWait(writeWait time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.WriteWait = writeWait
	}
}

func WithMaxMessageSize(maxMessageSize int64) ConnOption {
	return func(ch *Connect) {
		ch.Option.MaxMessageSize = maxMessageSize
	}
}

func WithPingPeriod(pingPeriod time.Duration) ConnOption {
	return func(ch *Connect) {
		ch.Option.PingPeriod = pingPeriod
	}
}

/*
 bucket options
*/
// ChannelSize   int
// 	RoomSize      int
// 	RoutineAmount uint64
// 	RoutineSize   int

func WithChannelSize(channelSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.ChannelSize = channelSize
	}
}

func WithRoomSize(roomSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoomSize = roomSize
	}
}
func WithRoutineAmount(routineAmount uint64) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoutineAmount = routineAmount
	}
}

func WithRoutineSize(routineSize int) ConnOption {
	return func(ch *Connect) {
		ch.Option.RoutineSize = routineSize
	}
}
