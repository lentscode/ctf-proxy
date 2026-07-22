// Command ctf-proxy is the local control and data-plane process for a CTF vulnbox.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/control"
	"github.com/lentscode/ctf-proxy/internal/observe"
)

const (
	defaultConfigPath  = "ctf-proxy.yaml"
	defaultControlAddr = "127.0.0.1:8081"
	defaultTokensFile  = ".tokens"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	configPath := os.Getenv("CTF_PROXY_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	if err := run(ctx, configPath); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("ctf-proxy stopped", "error", observe.SanitizeMessage(err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string) error {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	observation := observe.NewObserver(os.Stderr)
	defer observation.Close()
	store, err := config.OpenOrCreateStore(configPath)
	if err != nil {
		return err
	}
	manager, err := control.NewManager(store, configPath, observation)
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
	tokensFile := os.Getenv("CTF_PROXY_TOKENS_FILE")
	if tokensFile == "" {
		tokensFile = defaultTokensFile
	}
	tokens, err := control.LoadTokens(tokensFile)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		tokens = nil
	}
	if len(tokens) == 0 {
		token, err := control.GenerateToken()
		if err != nil {
			return fmt.Errorf("create initial control token: %w", err)
		}
		tokens = []string{token}
		if err := control.SaveTokens(tokensFile, tokens); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ctf-proxy: generated initial control token: %s\n", token)
	}
	server := &http.Server{Handler: control.NewHandler(manager, tokens, observation.Hub())}
	logger.Info("control API listening", "address", controlAddr)
	go func() { <-ctx.Done(); _ = server.Close() }()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
