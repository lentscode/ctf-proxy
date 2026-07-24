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

// TestRunExchangesConfiguredPayload verifies repeated polling and response checks.
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

// TestExchangeRejectsMismatchedResponse covers short and incorrect responses.
func TestExchangeRejectsMismatchedResponse(t *testing.T) {
	testCases := []struct {
		name     string
		payload  string
		response string
		wantErr  string
	}{
		{name: "different response", payload: "hello", response: "wrong", wantErr: "did not match"},
		{name: "empty response", payload: "hello", response: "", wantErr: "EOF"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			t.Cleanup(func() { _ = listener.Close() })
			go func() {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				defer conn.Close()
				payload := make([]byte, len(testCase.payload))
				_, _ = io.ReadFull(conn, payload)
				_, _ = conn.Write([]byte(testCase.response))
			}()

			err = exchange(context.Background(), listener.Addr().String(), []byte(testCase.payload), time.Second)
			require.ErrorContains(t, err, testCase.wantErr)
		})
	}
}
