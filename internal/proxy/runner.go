package proxy

import (
	"context"
	"net"
)

// Runner serves an already-bound listener and stops it when its context is
// cancelled. Binding is owned by the control plane so it can report port
// conflicts before committing configuration changes.
type Runner interface {
	Serve(context.Context, net.Listener) error
}

var (
	_ Runner = (*TCPProxy)(nil)
	_ Runner = (*HTTPProxy)(nil)
)
