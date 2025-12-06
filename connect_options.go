package sloth

// connect options

type IRpcOption func(IRpc)
type ServerRpcOption func(*ServerRpc)
type ClientRpcOption func(*ClientRpc)

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
