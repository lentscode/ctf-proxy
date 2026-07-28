// Command ctf-proxy is the local control and data-plane process for a CTF vulnbox.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lentscode/ctf-proxy/internal/compose"
	"github.com/lentscode/ctf-proxy/internal/config"
	"github.com/lentscode/ctf-proxy/internal/control"
	"github.com/lentscode/ctf-proxy/internal/observe"
)

const (
	defaultConfigPath  = "ctf-proxy.yaml"
	defaultControlAddr = "127.0.0.1:8081"
	defaultTokensFile  = ".tokens"
	defaultComposeRoot = "/root"
	composeFilesEnv    = "CTF_PROXY_COMPOSE_FILE_NAMES"
)

// main starts the control plane and keeps the process alive until interrupted.
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

// run opens persistent state, starts managed proxies, and serves the loopback API.
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
	dashboardAddr := os.Getenv("CTF_PROXY_DASHBOARD_ADDR")
	if dashboardAddr != "" && os.Getenv(dashboardModeEnv) == "disabled" {
		return errors.New("CTF_PROXY_DASHBOARD_ADDR requires the embedded dashboard")
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
	composeRoot := os.Getenv("CTF_PROXY_COMPOSE_ROOT")
	if composeRoot == "" {
		composeRoot = defaultComposeRoot
	}
	composeFiles := append([]string(nil), compose.DefaultFileNames...)
	if extra := os.Getenv(composeFilesEnv); extra != "" {
		composeFiles = append(composeFiles, strings.Split(extra, ",")...)
	}
	composeManager := control.NewComposeManager(composeRoot, configPath, manager, composeFiles)
	handler, err := newServerHandler(control.NewHandlerWithScanAndConfigure(manager, tokens, observation.Hub(), composeManager))
	if err != nil {
		return err
	}
	logger.Info("control API listening", "address", controlAddr)
	listeners := []net.Listener{listener}
	if dashboardAddr != "" {
		dashboardListener, err := control.ListenDashboard(dashboardAddr)
		if err != nil {
			return err
		}
		listeners = append(listeners, dashboardListener)
		logger.Info("dashboard listening", "address", dashboardAddr)
	}
	return serveHTTPServers(ctx, handler, listeners...)
}

// serveHTTPServers serves one handler on every requested listener and closes
// them all when the process context ends or any server returns an error.
func serveHTTPServers(ctx context.Context, handler http.Handler, listeners ...net.Listener) error {
	servers := make([]*http.Server, len(listeners))
	results := make(chan error, len(listeners))
	for index, listener := range listeners {
		server := &http.Server{Handler: handler}
		servers[index] = server
		go func() { results <- server.Serve(listener) }()
	}
	stop := context.AfterFunc(ctx, func() {
		for _, server := range servers {
			_ = server.Close()
		}
	})
	defer stop()
	defer func() {
		for _, server := range servers {
			_ = server.Close()
		}
	}()

	err := <-results
	if ctx.Err() != nil && errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
