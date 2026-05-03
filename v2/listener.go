package sloth

import (
	"crypto/tls"
	"net"
)

// block can be nil if the caller wishes to skip encryption in kcp.
// tlsConfig can be nil iff we are not using network "quic".
func (s *Connect) makeListener(network, address string) (ln net.Listener, err error) {
	if s.tlsConfig == nil {
		ln, err = net.Listen(network, address)
	} else {
		ln, err = tls.Listen(network, address, s.tlsConfig)
	}
	return ln, err
}
