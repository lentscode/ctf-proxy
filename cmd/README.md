# Test utilities

These commands provide simple local upstreams and clients for exercising a
`ctf-proxy` deployment.

## TCP echo server

Start a TCP upstream that echoes every received byte back to its client:

```sh
go run ./cmd/echo-server -listen 127.0.0.1:9000
```

Point a TCP proxy's `upstream` at `127.0.0.1:9000`, then connect to the
proxy's public listener with a TCP client such as `nc`.

## TCP poller

Connect to a TCP proxy, send a payload, and verify that the same bytes are
returned immediately and then every second until interrupted:

```sh
go run ./cmd/tcp-poller -address 127.0.0.1:9000
```

Useful optional flags include `-interval 5s`, `-count 10`, `-message payload`,
and `-timeout 2s`. The command prints only attempt numbers and byte counts; it
does not print the sent or received payload.
