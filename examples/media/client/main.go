package main

// 媒体代理的客户端样例（内网侧）。
//
// 它做两件事：
//  1. 在本机起一个普通的 http.FileServer（-root 指定根目录）；
//  2. 注册 "http" 服务的 Do 方法：把服务端转发过来的 HTTP 报文还原成请求，
//     打给本地 FileServer，再把响应报文原样返回。
//
// 于是 Range / 206 / Content-Type / MIME / 304 全部是 http.FileServer 的现成能力，
// 客户端不用自己实现——和服务端网关一样，只负责搬运。
//
// 服务名 "media" 是它在网关上的名字（/m/media/...），由 v1.Reg 登记；
// RPC 服务名 "http" 是方法前缀（http.Do）。两者是不同维度的名字。
//
// 运行（先起 examples/media/server）：
//
//	go run ./examples/media/client -root ./.media

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/types/auth"
	"github.com/w6xian/tlv"
)

const (
	rpcAddr = "localhost:8991"
	// serviceName 网关 URL 里的服务名：/m/media/...
	serviceName = "media"
	// rpcService 客户端注册的 RPC 服务名，方法是 http.Do
	rpcService = "http"
)

// maxRespBytes 无 Range 的大响应会让报文整包进内存，这里设个上限。
// 正常播放靠浏览器发 Range（每个分片才几百 KB），不会触发。
const maxRespBytes = 32 << 20

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	root := flag.String("root", "./.media", "对外暴露的本地媒体根目录")
	flag.Parse()

	if err := prepareRoot(*root); err != nil {
		sloth.Errorw(ctx, "prepare media root failed", "err", err)
		return
	}

	// ① 本地 HTTP 服务：只监听回环 + 随机端口，不对外暴露。
	//    FileServer 自带 Range / Content-Type / Last-Modified / If-None-Match。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		sloth.Errorw(ctx, "listen local http failed", "err", err)
		return
	}
	local := "http://" + ln.Addr().String()
	fmux := http.NewServeMux()
	fmux.Handle("/", http.FileServer(http.Dir(*root)))
	fsrv := &http.Server{Handler: fmux, ReadHeaderTimeout: 15 * time.Second}
	go func() {
		if err := fsrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			sloth.Errorw(ctx, "local http server exited", "err", err)
		}
	}()
	sloth.Infow(ctx, "local file server started", "root", *root, "addr", local)

	// ② 注册转发服务：服务端网关调 "http.Do" 时走到 ProxyService.Do
	client := sloth.DefaultClient()
	conn := sloth.ClientConn(client)
	proxy := &ProxyService{local: ln.Addr().String(), rt: http.DefaultTransport}
	if err := conn.Register(rpcService, proxy, ""); err != nil {
		sloth.Errorw(ctx, "register service failed", "err", err)
		return
	}

	// ③ 连服务端（tcp 的 Dial 不阻塞，返回即已连上）
	if err := conn.Dial(ctx, sloth.TCP, rpcAddr); err != nil {
		sloth.Errorw(ctx, "dial failed", "err", err)
		return
	}
	defer conn.Close()

	// ④ Sign（正数 userId，收推送）+ Reg（负数 userId，当服务提供者）
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		data, err := client.Call(ctx, "v1.Sign", []byte("sign"))
		if err != nil {
			sloth.Errorw(ctx, "sign failed", "err", err)
			continue
		}
		ai := &auth.AuthInfo{}
		if err := tlv.Json2Struct(data, ai); err != nil {
			sloth.Errorw(ctx, "sign decode failed", "err", err)
			continue
		}
		if err := client.SetAuthInfo(ai); err != nil {
			sloth.Errorw(ctx, "set auth info failed", "err", err)
			continue
		}

		data, err = client.Call(ctx, "v1.Reg", serviceName)
		if err != nil {
			sloth.Errorw(ctx, "reg failed", "err", err)
			continue
		}
		info := &auth.AuthInfo{}
		if err := tlv.Json2Struct(data, info); err != nil {
			sloth.Errorw(ctx, "reg decode failed", "err", err)
			continue
		}
		sloth.Infow(ctx, "registered", "service", serviceName, "userId", info.UserId,
			"hint", "curl -r 0-99 http://localhost:8080/m/"+serviceName+"/sample.bin")
		break
	}

	select {}
}

// ProxyService 把服务端转发来的 HTTP 报文打给本地 HTTP 服务。
type ProxyService struct {
	// local 本地 http 服务的 host:port
	local string
	rt    http.RoundTripper
}

// Do 收到一个完整的 HTTP 请求报文，返回一个完整的 HTTP 响应报文。
func (p *ProxyService) Do(ctx context.Context, raw []byte) ([]byte, error) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(raw)))
	if err != nil {
		return nil, fmt.Errorf("bad request: %w", err)
	}
	// RoundTrip 要求 RequestURI 为空（那是服务端请求才有的字段）
	req.RequestURI = ""
	req.URL.Scheme = "http"
	req.URL.Host = p.local

	resp, err := p.rt.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("round trip: %w", err)
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxRespBytes {
		return nil, fmt.Errorf("response too large: %d bytes", resp.ContentLength)
	}

	var buf bytes.Buffer
	if err := resp.Write(&buf); err != nil {
		return nil, fmt.Errorf("write response: %w", err)
	}
	sloth.Infow(ctx, "proxied", "method", req.Method, "path", req.URL.Path,
		"status", resp.StatusCode, "bytes", buf.Len())
	return buf.Bytes(), nil
}

// wallpaperSrc 系统自带壁纸，直接拿来当"真实的图片素材"（没有 ffmpeg，
// 又想让浏览器看到一张真照片而不是合成色块）。
// 读不到就退回程序生成的渐变 JPEG，文件名不变。
const wallpaperSrc = `C:\Windows\Web\Wallpaper\Windows\img0.jpg`

// prepareRoot 准备好示例文件：真实使用时把 -root 指向自己的目录即可。
func prepareRoot(root string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"),
		[]byte("hello from the intranet\n"), 0o644); err != nil {
		return err
	}
	// sample.bin 1MB，内容可预测（i%251），方便用 Range 校验字节是否正确
	if err := writeOnce(filepath.Join(root, "sample.bin"), 1<<20, func(w io.Writer) error {
		buf := make([]byte, 1<<20)
		for i := range buf {
			buf[i] = byte(i % 251)
		}
		_, err := w.Write(buf)
		return err
	}); err != nil {
		return err
	}
	// wallpaper.jpg：优先复制系统壁纸，否则生成渐变图（文件名固定，页面写死它）
	if err := writeOnce(filepath.Join(root, "wallpaper.jpg"), 0, func(w io.Writer) error {
		if b, err := os.ReadFile(wallpaperSrc); err == nil {
			_, err := w.Write(b)
			return err
		}
		return jpeg.Encode(w, gradient(640, 360), &jpeg.Options{Quality: 85})
	}); err != nil {
		return err
	}
	// anim.gif：标准库就能编的动图，用来演示"连续帧"也能经同一条链路转发
	// （真实视频把 mp4/webm 放进 root 即可，播放与拖动都是 FileServer 的现成能力）
	return writeOnce(filepath.Join(root, "anim.gif"), 0, writeGIF)
}

// writeOnce 目标文件不存在时才写入；size>0 时用它判断已存在文件是否完整。
func writeOnce(path string, size int64, write func(io.Writer) error) error {
	if st, err := os.Stat(path); err == nil && (size == 0 || st.Size() == size) {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return write(f)
}

// gradient 生成一张渐变图（拿不到系统壁纸时的兜底素材）。
func gradient(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / w), G: uint8(y * 255 / h),
				B: uint8((x + y) % 256), A: 0xFF,
			})
		}
	}
	return img
}

// writeGIF 生成一张斜条纹滚动的动图（320x180，20 帧）。
func writeGIF(out io.Writer) error {
	const W, H, frames = 320, 180, 20
	pal := make(color.Palette, 256)
	for i := range pal {
		pal[i] = color.RGBA{R: uint8(i), G: uint8(255 - i), B: uint8((i * 3) % 256), A: 0xFF}
	}
	g := &gif.GIF{}
	for f := 0; f < frames; f++ {
		img := image.NewPaletted(image.Rect(0, 0, W, H), pal)
		for y := 0; y < H; y++ {
			for x := 0; x < W; x++ {
				img.SetColorIndex(x, y, uint8((x/3+y/2+f*8)%256))
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 8) // 单位 10ms → 约 80ms/帧
	}
	return gif.EncodeAll(out, g)
}
