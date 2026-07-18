package proxy

import "context"

// Runner starts a proxy and stops it when its context is cancelled.
type Runner interface {
	Start(context.Context) error
}

var (
	_ Runner = (*TCPProxy)(nil)
	_ Runner = (*HTTPProxy)(nil)
)
