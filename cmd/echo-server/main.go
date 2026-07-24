// Command echo-server runs a small TCP echo service for local proxy testing.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// main runs the local TCP echo-service test helper.
func main() {
	listenAddr := flag.String("listen", "127.0.0.1:9000", "TCP address to listen on")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *listenAddr); err != nil {
		slog.Error("echo server stopped", "error", err)
		os.Exit(1)
	}
}

// run listens for connections until the context is cancelled.
func run(ctx context.Context, listenAddr string) error {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	slog.Info("echo server listening", "address", listener.Addr().String())
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) && ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept connection: %w", err)
		}
		go echo(conn)
	}
}

// echo copies all bytes received on conn back to the same connection.
func echo(conn net.Conn) {
	defer conn.Close()
	_, _ = io.Copy(conn, conn)
}
