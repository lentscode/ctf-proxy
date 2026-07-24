package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lentscode/ctf-proxy/internal/filter"
	"github.com/lentscode/ctf-proxy/internal/observe"
)

var _ http.Handler = &HTTPProxy{}

// DefaultMaxFilterBodyBytes is the largest HTTP body made available to a
// body-dependent filter.
const DefaultMaxFilterBodyBytes int64 = 64 << 10

// HTTPProxy will forward HTTP requests to an upstream address.
type HTTPProxy struct {
	listenAddr  string
	upstreamUrl string

	slots chan struct{}

	transport *http.Transport
	upstream  *url.URL
	filters   *filter.Chain
	reporter  observe.Reporter
}

// NewHTTPProxy constructs an HTTP runner with a shared request budget and filter chain.
func NewHTTPProxy(listenAddr, upstreamUrl string, slots chan struct{}, filters *filter.Chain, reporters ...observe.Reporter) *HTTPProxy {
	var reporter observe.Reporter
	if len(reporters) > 0 {
		reporter = reporters[0]
	}
	if reporter == nil {
		reporter = observe.NopReporter{}
	}
	return &HTTPProxy{
		listenAddr:  listenAddr,
		upstreamUrl: upstreamUrl,
		slots:       slots,
		filters:     filters,
		reporter:    reporter,
	}
}

// Start binds the configured address and serves HTTP until stopped.
func (p *HTTPProxy) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", p.listenAddr)
	if err != nil {
		return err
	}

	return p.serve(ctx, listener)
}

// Serve forwards HTTP requests accepted from listener until ctx is cancelled.
func (p *HTTPProxy) Serve(ctx context.Context, listener net.Listener) error {
	return p.serve(ctx, listener)
}

// serve validates the upstream and runs the bounded HTTP server lifecycle.
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

// ServeHTTP filters and forwards one request, then filters the upstream response.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.acquireSlot() {
		http.Error(w, "proxy is busy", http.StatusServiceUnavailable)
		return
	}
	defer p.releaseSlot()

	requestMessage, err := p.requestFilterMessage(r)
	if err != nil {
		http.Error(w, "request body unavailable", http.StatusBadRequest)
		return
	}
	if p.filters.Evaluate(r.Context(), requestMessage).Action == filter.ActionReject {
		writeFilterRejection(w)
		return
	}

	outbound := r.Clone(r.Context())

	outbound.RequestURI = ""
	outbound.URL.Scheme = p.upstream.Scheme
	outbound.URL.Host = p.upstream.Host
	outbound.URL.User = p.upstream.User

	outbound.Host = p.upstream.Host

	removeHopByHopHeaders(outbound.Header)

	res, err := p.transport.RoundTrip(outbound)
	if err != nil {
		p.reporter.Report(observe.Event{Level: observe.LevelError, Component: observe.ComponentProxy, Kind: observe.KindProxyUpstreamUnavailable, Message: "HTTP upstream unavailable"})
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	responseMessage, err := p.responseFilterMessage(res)
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	if p.filters.Evaluate(r.Context(), responseMessage).Action == filter.ActionReject {
		writeFilterRejection(w)
		return
	}

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

// requestFilterMessage builds the request view and buffers its body when required.
func (p *HTTPProxy) requestFilterMessage(r *http.Request) (filter.Message, error) {
	message := filter.Message{
		Protocol:  filter.ProtocolHTTP,
		Direction: filter.DirectionRequest,
		HTTP: &filter.HTTPMessage{
			Method: r.Method,
			Path:   r.URL.RequestURI(),
			Header: r.Header,
		},
	}
	if !p.filters.NeedsHTTPBody(filter.DirectionRequest) || r.Body == nil {
		return message, nil
	}

	body, skipped, restored, err := inspectHTTPBody(r.Body)
	if err != nil {
		return filter.Message{}, err
	}
	r.Body = restored
	message.HTTP.Body = body
	message.HTTP.BodySkipped = skipped
	return message, nil
}

// responseFilterMessage builds the response view and buffers its body when required.
func (p *HTTPProxy) responseFilterMessage(res *http.Response) (filter.Message, error) {
	message := filter.Message{
		Protocol:  filter.ProtocolHTTP,
		Direction: filter.DirectionResponse,
		HTTP: &filter.HTTPMessage{
			StatusCode: res.StatusCode,
			Header:     res.Header,
		},
	}
	if !p.filters.NeedsHTTPBody(filter.DirectionResponse) || res.Body == nil {
		return message, nil
	}

	body, skipped, restored, err := inspectHTTPBody(res.Body)
	if err != nil {
		return filter.Message{}, err
	}
	res.Body = restored
	message.HTTP.Body = body
	message.HTTP.BodySkipped = skipped
	return message, nil
}

// inspectHTTPBody reads the bounded inspection prefix and restores the full stream.
func inspectHTTPBody(body io.ReadCloser) ([]byte, bool, io.ReadCloser, error) {
	data, err := io.ReadAll(io.LimitReader(body, DefaultMaxFilterBodyBytes+1))
	if err != nil {
		return nil, false, nil, err
	}
	if int64(len(data)) <= DefaultMaxFilterBodyBytes {
		if err := body.Close(); err != nil {
			return nil, false, nil, err
		}
		return data, false, io.NopCloser(bytes.NewReader(data)), nil
	}

	return nil, true, &prefixedReadCloser{
		Reader: io.MultiReader(bytes.NewReader(data), body),
		closer: body,
	}, nil
}

// prefixedReadCloser replays an inspected prefix before continuing with the source.
type prefixedReadCloser struct {
	io.Reader
	closer io.Closer
}

// Close closes the original body after the prefixed reader has been consumed.
func (r *prefixedReadCloser) Close() error {
	return r.closer.Close()
}

// writeFilterRejection returns the protocol-independent HTTP rejection response.
func writeFilterRejection(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, "request rejected by proxy")
}

// acquireSlot claims one request slot without waiting for capacity.
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

// releaseSlot returns a previously claimed request slot.
func (p *HTTPProxy) releaseSlot() {
	if p.slots != nil {
		<-p.slots
	}
}

// removeHopByHopHeaders strips headers that describe one transport connection.
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
