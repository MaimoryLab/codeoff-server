package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MaimoryLab/codeoff-server/internal/daemon"
)

func main() {
	var config daemon.Config
	flag.StringVar(&config.ListenAddr, "listen", daemon.DefaultListenAddr, "control API listen address")
	flag.BoolVar(&config.CFTunnel, "cf-tunnel", false, "enable Cloudflare Tunnel")
	flag.StringVar(&config.CFTunnelMode, "cf-tunnel-mode", daemon.DefaultTunnelMode, "Cloudflare Tunnel mode: quick or external")
	flag.StringVar(&config.CFTunnelURL, "cf-tunnel-url", "", "existing tunnel origin for external mode")
	flag.StringVar(&config.StatePath, "state", "", "daemon state file")
	flag.StringVar(&config.CodexPath, "codex", "", "codex executable path (default: PATH)")
	flag.StringVar(&config.CloudflaredPath, "cloudflared", "", "cloudflared executable path (default: PATH)")
	flag.Parse()

	service, err := daemon.New(config)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("starting daemon: listen=%s cf_tunnel=%t mode=%s", config.ListenAddr, config.CFTunnel, config.CFTunnelMode)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := service.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
