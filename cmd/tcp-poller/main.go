// Command tcp-poller repeatedly exchanges a payload with a TCP service for local proxy testing.
package main

import (
	"bytes"
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
	"time"
)

// main parses polling options and runs the local TCP test client.
func main() {
	address := flag.String("address", "", "TCP address to connect to (required)")
	message := flag.String("message", "ping", "payload to send and verify")
	interval := flag.Duration("interval", time.Second, "time between exchanges")
	timeout := flag.Duration("timeout", 5*time.Second, "per-exchange timeout")
	count := flag.Int("count", 0, "number of exchanges (0 runs until interrupted)")
	flag.Parse()

	if *address == "" || *message == "" || *interval <= 0 || *timeout <= 0 || *count < 0 {
		flag.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *address, []byte(*message), *interval, *timeout, *count); err != nil {
		slog.Error("TCP poller stopped", "error", err)
		os.Exit(1)
	}
}

// run performs the configured number of exchanges, waiting between attempts.
func run(ctx context.Context, address string, message []byte, interval, timeout time.Duration, count int) error {
	for attempt := 1; count == 0 || attempt <= count; attempt++ {
		if err := exchange(ctx, address, message, timeout); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("exchange failed", "attempt", attempt, "error", err)
		} else if err == nil {
			slog.Info("exchange completed", "attempt", attempt, "bytes", len(message))
		}
		if count != 0 && attempt == count {
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}

// exchange sends message to address and verifies that the response is identical.
func exchange(ctx context.Context, address string, message []byte, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := conn.Write(message); err != nil {
		return err
	}
	response := make([]byte, len(message))
	if _, err := io.ReadFull(conn, response); err != nil {
		return err
	}
	if !bytes.Equal(response, message) {
		return fmt.Errorf("received data did not match sent payload")
	}
	return nil
}
