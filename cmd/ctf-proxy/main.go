// Command ctf-proxy is the local control and data-plane process for a CTF vulnbox.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/control"
)

const (
	defaultConfigPath  = "ctf-proxy.yaml"
	defaultControlAddr = "127.0.0.1:8081"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CTF_PROXY_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	if err := run(ctx, configPath); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, configPath string) error {
	store, err := config.OpenOrCreateStore(configPath)
	if err != nil {
		return err
	}
	manager, err := control.NewManager(store, configPath)
	if err != nil {
		return err
	}
	if err := manager.Start(ctx); err != nil {
		return err
	}
	defer manager.Close()
	controlAddr := os.Getenv("CTF_PROXY_CONTROL_ADDR")
	if controlAddr == "" {
		controlAddr = defaultControlAddr
	}
	listener, err := control.ListenLoopback(controlAddr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: control.NewHandler(manager)}
	log.Printf("control API listening on http://%s", controlAddr)
	go func() { <-ctx.Done(); _ = server.Close() }()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
