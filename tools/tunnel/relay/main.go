package main

// relay 入口。
//
// 运行：
//
//	go build -o relay.exe .
//	./relay.exe -conf conf/relay.example.toml

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
	confPath := flag.String("conf", "conf/relay.toml", "配置文件路径")
	flag.Parse()

	var cfg Config
	if err := loadConfig(*confPath, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sloth.SetLogLevel("info")

	relay, err := NewRelay(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid config:", err)
		os.Exit(1)
	}

	server := sloth.DefaultServer()
	drpc := sloth.ServerConn(server)
	if err := drpc.Register("tunnel", &RelayService{r: relay}, "tunnel relay"); err != nil {
		fmt.Fprintln(os.Stderr, "register service:", err)
		os.Exit(1)
	}

	// agent 拨入链路：ws（可换成 wss，配合 WithTLSCertKey）。
	if err := drpc.Listen(ctx, sloth.WS, cfg.RPCAddr,
		option.WithServerHandleMessage(&relayHandler{r: relay})); err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- drpc.Serve() }()

	if err := relay.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "start public listeners:", err)
		_ = drpc.Close()
		os.Exit(1)
	}
	sloth.Infow(ctx, "relay started", "rpc", cfg.RPCAddr, "tunnels", len(cfg.Tunnels))

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-serveErr:
		if err != nil {
			sloth.Errorw(ctx, "serve exited with error", "err", err)
		}
	case s := <-sig:
		sloth.Infow(ctx, "shutdown signal received", "signal", s.String())
	}

	cancel()
	relay.Shutdown()
	if err := drpc.Close(); err != nil {
		sloth.Errorw(ctx, "close server failed", "err", err)
	}
}

func loadConfig(path string, cfg *Config) error {
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return err
	}
	if cfg.RPCAddr == "" {
		return fmt.Errorf("rpc_addr is required")
	}
	if len(cfg.Tunnels) == 0 {
		return fmt.Errorf("no tunnels configured")
	}
	return nil
}
