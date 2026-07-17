package proxy

import (
	"context"
	"io"
	"net"
	"sync"
)

type Proxy struct {
	listenAddr   string
	upstreamAddr string

	slots chan struct{}
}

func NewProxy(listenAddr, upstreamAddr string, slots chan struct{}) *Proxy {
	return &Proxy{
		listenAddr:   listenAddr,
		upstreamAddr: upstreamAddr,
		slots:        slots,
	}
}

func (p *Proxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}
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

func (p *Proxy) forward(client net.Conn) error {
	defer client.Close()

	//TODO(lentscode): add timeout
	upstream, err := net.Dial("tcp", p.upstreamAddr)
	if err != nil {
		return err
	}
	defer upstream.Close()

	errChan := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() {
		errChan <- p.copy(upstream, client)
	})
	wg.Go(func() {
		errChan <- p.copy(client, upstream)
	})

	firstCompleted := <-errChan

	_ = client.Close()
	_ = upstream.Close()

	wg.Wait()
	<-errChan

	//TODO(lentscode): better error handling
	if firstCompleted != nil {
		return firstCompleted
	}

	return nil
}

func (p *Proxy) copy(dst, src net.Conn) error {
	_, err := io.Copy(dst, src)
	return err
}
