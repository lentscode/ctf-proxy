package proxy

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCPProxyForwardsTCP(t *testing.T) {
	upstreamAddr := startEchoServer(t)

	p := NewTCPProxy("unused", upstreamAddr, make(chan struct{}, 1))
	proxyAddr, forwardDone := startTCPProxyOnce(t, p)

	client, err := net.DialTCP("tcp", nil, proxyAddr)
	require.NoError(t, err)
	defer client.Close()

	err = client.SetDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)

	payload := []byte("proxy test")
	_, err = client.Write(payload)
	require.NoError(t, err)

	err = client.CloseWrite()
	require.NoError(t, err)

	got, err := io.ReadAll(client)
	require.NoError(t, err)

	assert.Equal(t, string(payload), string(got))

	select {
	case err := <-forwardDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("proxy timeout")
	}
}

func TestTCPProxyServe(t *testing.T) {
	upstreamAddr := startEchoServer(t)
	proxyAddr, cancel, serveDone := startTCPProxy(t, upstreamAddr, make(chan struct{}, 1))

	client, err := net.DialTCP("tcp", nil, proxyAddr)
	require.NoError(t, err)
	defer client.Close()

	err = client.SetDeadline(time.Now().Add(time.Second))
	require.NoError(t, err)

	payload := []byte("proxy test")
	_, err = client.Write(payload)
	require.NoError(t, err)

	err = client.CloseWrite()
	require.NoError(t, err)

	got, err := io.ReadAll(client)
	require.NoError(t, err)

	assert.Equal(t, string(payload), string(got))
	cancel()

	requireProxyStopped(t, serveDone)
}

func TestTCPProxyServeClosesConnectionsWhenSlotsAreFull(t *testing.T) {
	upstreamListener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer upstreamListener.Close()

	upstreamAccepted := make(chan struct{})
	releaseUpstream := make(chan struct{})
	t.Cleanup(func() { close(releaseUpstream) })
	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		upstreamAccepted <- struct{}{}
		<-releaseUpstream
	}()

	proxyAddr, cancel, serveDone := startTCPProxy(t, upstreamListener.Addr().String(), make(chan struct{}, 1))
	defer cancel()

	firstClient, err := net.DialTCP("tcp", nil, proxyAddr)
	require.NoError(t, err)
	defer firstClient.Close()

	select {
	case <-upstreamAccepted:
	case <-time.After(time.Second):
		t.Fatal("upstream did not receive the first proxied connection")
	}

	secondClient, err := net.DialTCP("tcp", nil, proxyAddr)
	require.NoError(t, err)
	defer secondClient.Close()
	require.NoError(t, secondClient.SetReadDeadline(time.Now().Add(time.Second)))

	_, err = secondClient.Read(make([]byte, 1))
	assert.Error(t, err)

	cancel()
	requireProxyStopped(t, serveDone)
}

func startEchoServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

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

func startTCPProxyOnce(t *testing.T, p *TCPProxy) (*net.TCPAddr, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

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
	require.True(t, ok)

	return addr, forwardDone
}

func startTCPProxy(t *testing.T, upstreamAddr string, slots chan struct{}) (*net.TCPAddr, context.CancelFunc, <-chan error) {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	ctx, cancel := context.WithCancel(context.Background())
	proxy := NewTCPProxy("unused", upstreamAddr, slots)
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- proxy.serve(ctx, listener)
	}()

	return addr, cancel, serveDone
}
