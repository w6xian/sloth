package quic

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"time"

	quicgo "github.com/quic-go/quic-go"
)

// DefaultALPN QUIC 在 TLS 握手时协商的应用协议名。
//
// QUIC 把加密做进了传输层：**没有 TLS 就没有 QUIC**（这与 ws/wss 不同——
// 那里 TLS 是可选项）。因此 ALPN 是必填项：没有它，两端握手会直接失败。
// 用户没指定 NextProtos 时，本传输会填这个值。
const DefaultALPN = "sloth"

// defaultQuicConfig QUIC 传输的默认参数。
//
// MaxIdleTimeout：连接空闲上限，超过即断开。QUIC 是 UDP，没有 TCP 那样的
// 连接状态，靠 idle timeout 回收"对端已消失但没发 FIN"的连接。
// KeepAlivePeriod：小于 MaxIdleTimeout 才会真正发保活包，否则中间设备
// （NAT/防火墙）会静默丢掉 UDP 映射——移动网络下尤其常见。
func defaultQuicConfig() *quicgo.Config {
	return &quicgo.Config{
		MaxIdleTimeout:  60 * time.Second,
		KeepAlivePeriod: 15 * time.Second,
	}
}

// withALPN 补全 ALPN：用户没给就填 DefaultALPN。
//
// 返回的是副本：直接改用户传进来的 *tls.Config 会污染调用方持有的对象
// （同一个 config 常被多个监听地址复用）。
func withALPN(tlsConf *tls.Config) *tls.Config {
	if tlsConf == nil {
		return nil
	}
	if len(tlsConf.NextProtos) > 0 {
		return tlsConf
	}
	cp := tlsConf.Clone()
	cp.NextProtos = []string{DefaultALPN}
	return cp
}

// streamConn 把 QUIC 的一条 stream 补成 net.Conn。
//
// *quicgo.Stream 有 Read / Write / Close / SetDeadline，但没有 LocalAddr /
// RemoteAddr，因此并不满足 net.Conn。补上这两个方法后就能直接接进
// nrpc/stream 的通道——TCP 连接与 QUIC stream 从此走同一套代码。
type streamConn struct {
	*quicgo.Stream
	local, remote net.Addr
}

func (c *streamConn) LocalAddr() net.Addr  { return c.local }
func (c *streamConn) RemoteAddr() net.Addr { return c.remote }

// Close 关闭这条 stream。
//
// 注意 QUIC stream 是**半关闭**语义：Stream.Close() 只关发送方向，读方向
// 仍然开着——读循环会一直阻塞到 idle timeout 才退出。所以这里同时取消读，
// 让本端的读循环立刻返回（否则关闭一条连接要等 60s）。
func (c *streamConn) Close() error {
	c.Stream.CancelRead(quicgo.StreamErrorCode(0))
	return c.Stream.Close()
}

// Listener 把 QUIC 的"连接 + 多路 stream"模型适配成 net.Listener。
//
// 为什么要适配：上层 Connect.Listen 持有的是 net.Listener（ws / tcp 都跑在
// TCP 上），Serve 循环只认 Accept() (net.Conn, error)。QUIC 一次 Accept 拿到
// 的是**一条 QUIC 连接**，而真正承载 RPC 的是它上面的**流**——
// 一个 QUIC 连接可以同时开多条流。这里把所有流铺平到同一个 Accept 通道上：
// 每条流对上层就是一条独立连接（多路复用因此天然可用）。
type Listener struct {
	tr     *quicgo.Transport
	ln     *quicgo.Listener
	ctx    context.Context
	cancel context.CancelFunc
	accept chan net.Conn
	// closeAccept 保证 accept 通道只关一次（Accept 与 Close 可能并发）
	closeAccept sync.Once
	closed      atomic.Bool
}

// Listen 在 UDP 地址上监听 QUIC。
//
// tlsConf 不能为空：QUIC 的加密由 TLS 1.3 承担，没有证书就无法握手。
// 需要自签证书的场景见 examples/quic（运行时生成，仅示例用）。
func Listen(addr string, tlsConf *tls.Config, conf *quicgo.Config) (net.Listener, error) {
	if tlsConf == nil || len(tlsConf.Certificates) == 0 && tlsConf.GetCertificate == nil {
		return nil, errNoTLSCert
	}
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	if conf == nil {
		conf = defaultQuicConfig()
	}
	tr := &quicgo.Transport{Conn: udpConn}
	ln, err := tr.Listen(withALPN(tlsConf), conf)
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		tr:     tr,
		ln:     ln,
		ctx:    ctx,
		cancel: cancel,
		accept: make(chan net.Conn, 16),
	}
	go l.acceptLoop()
	return l, nil
}

// Accept 返回下一条可用的 QUIC stream（已补成 net.Conn）。
func (l *Listener) Accept() (net.Conn, error) {
	conn, ok := <-l.accept
	if !ok {
		// 通道关闭：listener 已 Close，用标准错误让上层判定为"正常退出"
		return nil, net.ErrClosed
	}
	return conn, nil
}

// Close 关闭监听器与底层 UDP socket。
func (l *Listener) Close() error {
	l.closeAccept.Do(func() {
		l.closed.Store(true)
		l.cancel()
		_ = l.ln.Close()
		close(l.accept)
		// UDP socket 必须显式关：只关 listener 不会释放端口
		_ = l.tr.Close()
	})
	return nil
}

// Addr 返回 UDP socket 的本地地址（端口在此）。
func (l *Listener) Addr() net.Addr {
	if l.tr == nil || l.tr.Conn == nil {
		return nil
	}
	return l.tr.Conn.LocalAddr()
}

// acceptLoop 接 QUIC 连接；每条连接的流由 serveConn 继续铺平。
func (l *Listener) acceptLoop() {
	for {
		qconn, err := l.ln.Accept(l.ctx)
		if err != nil {
			// listener 关闭或 ctx 取消（正常退出路径）
			l.closeAccept.Do(func() {
				l.closed.Store(true)
				close(l.accept)
			})
			return
		}
		go l.serveConn(qconn)
	}
}

// serveConn 把一条 QUIC 连接上的所有流依次交给 Accept。
// 连接断开（对端关闭 / idle timeout）时退出，不会泄漏 goroutine：
// AcceptStream 在连接结束时立即返回错误。
func (l *Listener) serveConn(qconn *quicgo.Conn) {
	for {
		st, err := qconn.AcceptStream(l.ctx)
		if err != nil {
			return
		}
		conn := &streamConn{
			Stream: st,
			local:  qconn.LocalAddr(),
			remote: qconn.RemoteAddr(),
		}
		select {
		case l.accept <- conn:
		case <-l.ctx.Done():
			_ = conn.Close()
			return
		}
	}
}
