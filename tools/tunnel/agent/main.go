package main

// agent 入口。
//
// 运行：
//
//	go build -o agent.exe .
//	./agent.exe -conf conf/agent.example.toml
//
// 进程长驻：断线由 sloth ws 客户端自动重连，重连后自动重新登记。

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/w6xian/sloth/v4"
	"github.com/w6xian/sloth/v4/option"
)

func main() {
	confPath := flag.String("conf", "conf/agent.toml", "配置文件路径")
	flag.Parse()

	var cfg Config
	if err := loadConfig(*confPath, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	client := sloth.DefaultClient()
	// 关键：把本端 codec 设为恒等。
	// Send/Call 默认会先把参数经 ag.Encoder 包一层 ag 帧（magic ":p"），而 relay
	// 侧 readPump 的 tlvValue 兜底会把 ag 帧误当 TLV 帧撕掉前几个字节——大帧被
	// 无声截断损毁。恒等编码后隧道帧原样上线；relay 收到的 0xC7 帧因 TLV 的
	// 4 字节长度字段必然越界而原样透传（见 proto.UnwrapEnvelope 注释）。
	// RPC 回包同理：服务端回的是 tlv.Json 帧（非 ag 帧），ag.Decoder 本来就是
	// 原样透传，恒等解码行为等价。
	client.SetEncoder(func(v any) ([]byte, error) {
		b, ok := v.([]byte)
		if !ok {
			return nil, fmt.Errorf("tunnel agent: identity encoder only accepts []byte, got %T", v)
		}
		return b, nil
	})
	client.SetDecoder(func(b []byte) ([]byte, error) {
		return b, nil
	})
	agent, err := NewAgent(ctx, cfg, client)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid config:", err)
		os.Exit(1)
	}

	conn := sloth.ClientConn(client)
	// Dial（ws 的 ListenAndServe）会阻塞并内部自动重连，直到 ctx 取消。
	go func() {
		if err := conn.Dial(ctx, sloth.WS, cfg.Relay,
			option.WithClientHandleMessage(&agentHandler{a: agent})); err != nil && ctx.Err() == nil {
			sloth.Errorw(ctx, "dial relay exited with error", "relay", cfg.Relay, "err", err)
			cancel()
		}
	}()
	sloth.Infow(ctx, "agent started", "relay", cfg.Relay,
		"name", cfg.Name, "tunnels", len(cfg.Tunnels))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	sloth.Infow(ctx, "shutdown signal received")
	cancel()
	agent.teardown()
	_ = conn.Close()
}

func loadConfig(path string, cfg *Config) error {
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return err
	}
	if cfg.Relay == "" {
		return fmt.Errorf("relay is required")
	}
	if len(cfg.Tunnels) == 0 {
		return fmt.Errorf("no tunnels configured")
	}
	return nil
}
