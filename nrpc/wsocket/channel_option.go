package wsocket

import "fmt"

type ChannelServerOption func(ch *WsChannelServer)
type ChannelClientOption func(s *WsChannelClient)

func WithAddr(addr string) ChannelClientOption {
	return func(s *WsChannelClient) {
		s.addr = addr
		fmt.Println("WithAddr:", addr)
	}
}
