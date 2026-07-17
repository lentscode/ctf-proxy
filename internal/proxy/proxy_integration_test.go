package proxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProxyForwardsTCP(t *testing.T) {
	a := assert.New(t)
	upstreamAddr := startEchoServer(t)

	p := NewProxy("unused", upstreamAddr, make(chan struct{}, 1))
	proxyAddr, forwardDone := startProxyOnce(t, p)

	client, err := net.DialTCP("tcp", nil, proxyAddr)
	a.NoError(err)
	defer client.Close()

	err = client.SetDeadline(time.Now().Add(time.Second))
	a.NoError(err)

	payload := []byte("proxy test")
	_, err = client.Write(payload)
	a.NoError(err)

	err = client.CloseWrite()
	a.NoError(err)

	got, err := io.ReadAll(client)
	a.NoError(err)

	a.Equal(string(payload), string(got))

	select {
	case err := <-forwardDone:
		a.NoError(err)
	case <-time.After(time.Second):
		t.Fatal("proxy timeout")
	}
}

func TestProxyServe(t *testing.T) {
	a := assert.New(t)
	upstreamAddr := startEchoServer(t)
	listener, err := net.Listen("tcp", "localhost:0")
	a.NoError(err)
	proxyAddr, ok := listener.Addr().(*net.TCPAddr)
	a.True(ok)

	ctx, cancel := context.WithCancel(context.Background())

	p := NewProxy("unused", upstreamAddr, make(chan struct{}, 1))
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- p.serve(ctx, listener)
	}()

	client, err := net.DialTCP("tcp", nil, proxyAddr)
	a.NoError(err)
	defer client.Close()

	err = client.SetDeadline(time.Now().Add(time.Second))
	a.NoError(err)

	payload := []byte("proxy test")
	_, err = client.Write(payload)
	a.NoError(err)

	err = client.CloseWrite()
	a.NoError(err)

	got, err := io.ReadAll(client)
	a.NoError(err)

	a.Equal(string(payload), string(got))
	cancel()

	select {
	case err := <-serveDone:
		a.NoError(err)
	case <-time.After(time.Second):
		t.Fatal("proxy timeout")
	}
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	a := assert.New(t)

	listener, err := net.Listen("tcp", "localhost:0")
	a.NoError(err)

	t.Cleanup(func() {
		_ = listener.Close()
	})

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				buffer := make([]byte, 1024)

				n, _ := conn.Read(buffer)
				if n > 0 {
					_, _ = conn.Write(buffer[:n])
				}
			}(conn)
		}
	}()

	return listener.Addr().String()
}

func startProxyOnce(t *testing.T, p *Proxy) (*net.TCPAddr, <-chan error) {
	t.Helper()
	a := assert.New(t)

	listener, err := net.Listen("tcp", "localhost:0")
	a.NoError(err)

	t.Cleanup(func() {
		_ = listener.Close()
	})

	forwardDone := make(chan error, 1)

	go func() {
		client, err := listener.Accept()
		if err != nil {
			forwardDone <- err
			return
		}

		forwardDone <- p.forward(client)
	}()

	addr, ok := listener.Addr().(*net.TCPAddr)
	a.True(ok)

	return addr, forwardDone
}
