package wsocket

import (
	"time"
)

type WsChannelOption func(ch *WsChannel)

func WithPongTimeout(timeout time.Duration) WsChannelOption {
	return func(ch *WsChannel) {
		ch.pongTimeout = timeout
	}
}

func WithWriteWait(writeWait time.Duration) WsChannelOption {
	return func(ch *WsChannel) {
		ch.writeWait = writeWait
	}
}

func WithMaxMessageSize(maxMessageSize int64) WsChannelOption {
	return func(ch *WsChannel) {
		ch.maxMessageSize = maxMessageSize
	}
}

func WithPingPeriod(pingPeriod time.Duration) WsChannelOption {
	return func(ch *WsChannel) {
		ch.pingPeriod = pingPeriod
	}
}
