// dev/target — 本地联调用的"内网服务"。
//
// 提供最小的 TCP/HTTP 文件服务 + UDP echo 服务，用于单机模拟
// 访客 → relay → agent → 本地服务 的全链路。
//
//	go run ./dev/target
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dir := ".devfiles"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	// 固定写一个可校验内容的样例文件（已存在则不覆盖）
	p := filepath.Join(dir, "hello.txt")
	if _, err := os.Stat(p); os.IsNotExist(err) {
		_ = os.WriteFile(p, []byte("hello through the tunnel\n"), 0o644)
	}

	httpAddr := "127.0.0.1:8080"
	udpAddr := "127.0.0.1:8081"
	fmt.Println("dev target listening on http://" + httpAddr + "/ (dir: " + dir + ")")
	fmt.Println("dev target listening on udp://" + udpAddr + "/ (echo, reply uppercased)")
	go udpEcho(udpAddr)
	if err := http.ListenAndServe(httpAddr, http.FileServer(http.Dir(dir))); err != nil {
		fmt.Fprintln(os.Stderr, "serve:", err)
		os.Exit(1)
	}
}

// udpEcho 收到什么回什么（大写化以便肉眼区分往返），用于验证 UDP 隧道。
func udpEcho(addr string) {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "udp listen:", err)
		os.Exit(1)
	}
	buf := make([]byte, 65535)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		reply := strings.ToUpper(string(buf[:n]))
		_, _ = pc.WriteTo([]byte(reply), from)
	}
}
