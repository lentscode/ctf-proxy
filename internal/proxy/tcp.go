package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/lentscode/ctf-proxy/internal/filter"
)

const tcpFilterBufferSize = 32 << 10

var errTCPFilterRejected = errors.New("TCP filter rejected traffic")

// TCPProxy forwards raw TCP connections to an upstream address.
type TCPProxy struct {
	listenAddr   string
	upstreamAddr string

	slots chan struct{}

	filters *filter.Chain
}

func NewTCPProxy(listenAddr, upstreamAddr string, slots chan struct{}, filters *filter.Chain) *TCPProxy {
	return &TCPProxy{
		listenAddr:   listenAddr,
		upstreamAddr: upstreamAddr,
		slots:        slots,
		filters:      filters,
	}
}

func (p *TCPProxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}

	return p.serve(ctx, listener)
}

func (p *TCPProxy) serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()

	cleanUp := context.AfterFunc(ctx, func() {
		_ = listener.Close()
	})
	defer cleanUp()

	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}

		select {
		case p.slots <- struct{}{}:
			go func() {
				defer func() { <-p.slots }()
				_ = p.forward(client)
			}()
		default:
			_ = client.Close()
		}
	}
}

func (p *TCPProxy) forward(client net.Conn) error {
	defer client.Close()

	//TODO(lentscode): add timeout
	upstream, err := net.Dial("tcp", p.upstreamAddr)
	if err != nil {
		return err
	}
	defer upstream.Close()

	connection := filter.ConnectionInfo{
		LocalAddr:  client.LocalAddr().String(),
		RemoteAddr: client.RemoteAddr().String(),
	}
	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		err := p.copy(upstream, client, filter.DirectionRequest, connection)
		if errors.Is(err, errTCPFilterRejected) {
			_ = client.Close()
			_ = upstream.Close()
		}
		if err == nil {
			closeWrite(upstream)
		}
		errChan <- err
	})
	wg.Go(func() {
		err := p.copy(client, upstream, filter.DirectionResponse, connection)
		if errors.Is(err, errTCPFilterRejected) {
			_ = client.Close()
			_ = upstream.Close()
		}
		if err == nil {
			closeWrite(client)
		}
		errChan <- err
	})

	wg.Wait()

	firstErr := <-errChan
	secondErr := <-errChan

	if errors.Is(firstErr, errTCPFilterRejected) {
		return firstErr
	}
	if errors.Is(secondErr, errTCPFilterRejected) {
		return secondErr
	}
	if firstErr != nil {
		return firstErr
	}

	return secondErr
}

func (p *TCPProxy) copy(dst, src net.Conn, direction filter.Direction, connection filter.ConnectionInfo) error {
	buffer := make([]byte, tcpFilterBufferSize)
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			decision := p.filters.Evaluate(context.Background(), filter.Message{
				Protocol:   filter.ProtocolTCP,
				Direction:  direction,
				Connection: connection,
				TCP:        &filter.TCPMessage{Data: buffer[:n]},
			})
			if decision.Action == filter.ActionReject {
				return errTCPFilterRejected
			}
			if err := writeAll(dst, buffer[:n]); err != nil {
				return err
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

func writeAll(dst io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func closeWrite(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
		return
	}

	_ = conn.Close()
}
