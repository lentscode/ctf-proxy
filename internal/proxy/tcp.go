package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/lentscode/ctf-proxy/internal/metrics"
	"github.com/lentscode/ctf-proxy/internal/observe"
)

const tcpFilterBufferSize = 32 << 10

var errTCPFilterRejected = errors.New("TCP filter rejected traffic")

// TCPOptions controls TCP connection establishment and per-operation deadlines.
// Zero values preserve the previous unbounded behavior.
type TCPOptions struct {
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// TCPProxy forwards raw TCP connections to an upstream address.
type TCPProxy struct {
	listenAddr   string
	upstreamAddr string
	options      TCPOptions

	slots chan struct{}

	filters  *filter.Chain
	reporter observe.Reporter
	metrics  metrics.Recorder
}

// SetMetrics attaches a proxy-bound aggregate recorder.
func (p *TCPProxy) SetMetrics(recorder metrics.Recorder) { p.metrics = recorder }

// NewTCPProxy constructs a TCP runner with a shared connection budget and filter chain.
func NewTCPProxy(listenAddr, upstreamAddr string, slots chan struct{}, filters *filter.Chain, reporters ...observe.Reporter) *TCPProxy {
	return NewTCPProxyWithOptions(listenAddr, upstreamAddr, slots, filters, TCPOptions{}, reporters...)
}

// NewTCPProxyWithOptions constructs a TCP runner with explicit deadline options.
func NewTCPProxyWithOptions(listenAddr, upstreamAddr string, slots chan struct{}, filters *filter.Chain, options TCPOptions, reporters ...observe.Reporter) *TCPProxy {
	var reporter observe.Reporter
	if len(reporters) > 0 {
		reporter = reporters[0]
	}
	if reporter == nil {
		reporter = observe.NopReporter{}
	}
	return &TCPProxy{
		listenAddr:   listenAddr,
		upstreamAddr: upstreamAddr,
		options:      options,
		slots:        slots,
		filters:      filters,
		reporter:     reporter,
	}
}

// Start binds the configured address and serves TCP connections until stopped.
func (p *TCPProxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}

	return p.serve(ctx, listener)
}

// Serve forwards connections accepted from listener until ctx is cancelled.
func (p *TCPProxy) Serve(ctx context.Context, listener net.Listener) error {
	return p.serve(ctx, listener)
}

// serve accepts clients, enforces the connection budget, and watches ctx.
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
				p.metrics.AcceptedConnection()
				defer p.metrics.ClosedConnection()
				_ = p.forward(client)
			}()
		default:
			p.metrics.Reject(true)
			_ = client.Close()
		}
	}
}

// forward connects one client to the upstream and copies both directions.
func (p *TCPProxy) forward(client net.Conn) error {
	defer client.Close()

	upstream, err := (&net.Dialer{Timeout: p.options.DialTimeout}).Dial("tcp", p.upstreamAddr)
	if err != nil {
		p.metrics.UpstreamFailure()
		p.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentProxy, Kind: observe.KindProxyUpstreamUnavailable, Message: "TCP upstream unavailable"})
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

// copy filters and forwards chunks in one direction, preserving half-close semantics.
func (p *TCPProxy) copy(dst, src net.Conn, direction filter.Direction, connection filter.ConnectionInfo) error {
	buffer := make([]byte, tcpFilterBufferSize)
	for {
		if p.options.ReadTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(p.options.ReadTimeout))
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			p.metrics.Chunk(direction == filter.DirectionResponse)
			decision := p.filters.Evaluate(context.Background(), filter.Message{
				Protocol:   filter.ProtocolTCP,
				Direction:  direction,
				Connection: connection,
				TCP:        &filter.TCPMessage{Data: buffer[:n]},
			})
			if decision.Action == filter.ActionReject {
				p.metrics.Reject(false)
				return errTCPFilterRejected
			}
			if p.options.WriteTimeout > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(p.options.WriteTimeout))
			}
			if err := writeAllCounting(dst, buffer[:n], func(written int) { p.metrics.Bytes(direction == filter.DirectionResponse, written) }); err != nil {
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

func writeAllCounting(dst io.Writer, data []byte, count func(int)) error {
	for len(data) > 0 {
		n, err := dst.Write(data)
		if n > 0 {
			count(n)
		}
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

// closeWrite half-closes a TCP connection when supported by its concrete type.
func closeWrite(conn net.Conn) {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.CloseWrite()
		return
	}

	_ = conn.Close()
}
