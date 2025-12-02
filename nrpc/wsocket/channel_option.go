package wsocket

import (
	"time"
)

type ChannelOption func(ch *Channel)

func WithPongTimeout(timeout time.Duration) ChannelOption {
	return func(ch *Channel) {
		ch.pongTimeout = timeout
	}
}

func WithWriteWait(writeWait time.Duration) ChannelOption {
	return func(ch *Channel) {
		ch.writeWait = writeWait
	}
}

func WithMaxMessageSize(maxMessageSize int64) ChannelOption {
	return func(ch *Channel) {
		ch.maxMessageSize = maxMessageSize
	}
}

func WithPingPeriod(pingPeriod time.Duration) ChannelOption {
	return func(ch *Channel) {
		ch.pingPeriod = pingPeriod
	}
}

func WithErrorHandler(errHandler func(err error)) ChannelOption {
	return func(ch *Channel) {
		ch.errHandler = errHandler
	}
}
