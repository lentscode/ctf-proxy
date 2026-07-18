package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var _ http.Handler = &HTTPProxy{}

// HTTPProxy will forward HTTP requests to an upstream address.
type HTTPProxy struct {
	listenAddr  string
	upstreamUrl string

	slots chan struct{}

	transport *http.Transport
	upstream  *url.URL
}

func NewHTTPProxy(listenAddr, upstreamUrl string, slots chan struct{}) *HTTPProxy {
	return &HTTPProxy{
		listenAddr:  listenAddr,
		upstreamUrl: upstreamUrl,
		slots:       slots,
	}
}

func (p *HTTPProxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}

	return p.serve(ctx, listener)
}

func (p *HTTPProxy) serve(ctx context.Context, listener net.Listener) error {
	defer listener.Close()

	upstream, err := url.Parse(p.upstreamUrl)
	if err != nil {
		return fmt.Errorf("parse upstream URL: %w", err)
	}
	if upstream.Scheme != "http" && upstream.Scheme != "https" {
		return fmt.Errorf("upstream URL must use http or https, got %q", upstream.Scheme)
	}
	if upstream.Host == "" {
		return errors.New("upstream URL must include a host")
	}
	if upstream.Path != "" && upstream.Path != "/" {
		return errors.New("upstream URL must not include a path")
	}

	p.upstream = upstream

	//TODO(lentscode): replace with user defined values
	p.transport = &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		MaxConnsPerHost:       64,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
	defer p.transport.CloseIdleConnections()

	//TODO(lentscode): replace with user defined values
	server := &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	stop := context.AfterFunc(ctx, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(shutdownCtx)
		p.transport.CloseIdleConnections()
	})
	defer stop()

	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}

func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.acquireSlot() {
		http.Error(w, "proxy is busy", http.StatusServiceUnavailable)
		return
	}
	defer p.releaseSlot()

	outbound := r.Clone(r.Context())

	outbound.RequestURI = ""
	outbound.URL.Scheme = p.upstream.Scheme
	outbound.URL.Host = p.upstream.Host
	outbound.URL.User = p.upstream.User

	outbound.Host = p.upstream.Host

	removeHopByHopHeaders(outbound.Header)

	res, err := p.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	removeHopByHopHeaders(res.Header)

	for k, values := range res.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(res.StatusCode)

	if _, err := io.Copy(w, res.Body); err != nil {
		//TODO(lentscode): error handling
		return
	}
}

func (p *HTTPProxy) acquireSlot() bool {
	if p.slots == nil {
		return true
	}

	select {
	case p.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (p *HTTPProxy) releaseSlot() {
	if p.slots != nil {
		<-p.slots
	}
}

func removeHopByHopHeaders(h http.Header) {
	// Connection may nominate extra header names that must not be forwarded.
	for _, value := range h.Values("Connection") {
		for name := range strings.SplitSeq(value, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}

	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"TE",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		h.Del(name)
	}
}
