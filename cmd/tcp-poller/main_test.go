package main

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunExchangesConfiguredPayload(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	var accepted sync.WaitGroup
	accepted.Add(2)
	go func() {
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				defer accepted.Done()
				payload := make([]byte, len("test-payload"))
				_, _ = io.ReadFull(conn, payload)
				_, _ = conn.Write(payload)
			}()
		}
	}()

	require.NoError(t, run(context.Background(), listener.Addr().String(), []byte("test-payload"), time.Millisecond, time.Second, 2))
	accepted.Wait()
}

func TestExchangeRejectsMismatchedResponse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		payload := make([]byte, len("hello"))
		_, _ = io.ReadFull(conn, payload)
		_, _ = conn.Write([]byte("wrong"))
	}()

	err = exchange(context.Background(), listener.Addr().String(), []byte("hello"), time.Second)
	require.ErrorContains(t, err, "did not match")
}
