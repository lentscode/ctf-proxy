# ctf-proxy

A learning-oriented, low-overhead layer-4 proxy for Attack & Defense CTF
vulnerability boxes.

The project will provide TCP and HTTP forwarding with bidirectional filtering and a
local web dashboard. See [agents.md](agents.md) for the agreed goals, constraints,
and intended architecture.

## Configuration

`ctf-proxy` reads `./ctf-proxy.yaml` by default. Set `CTF_PROXY_CONFIG` to
choose a different file. Relative filter paths are resolved relative to the
main configuration file.

```yaml
version: 1
max_connections: 128 # per proxy; omit to use the default
proxies:
  - name: web
    active: true
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
    filter_files:
      - filters/web.yaml

  - name: challenge
    active: false # staged in configuration, but not started
    protocol: tcp
    listen: ":31337"
    upstream: "127.0.0.1:31338"
    filter_files:
      - filters/challenge.yaml
```

The configuration is strictly validated before any proxy is started. The
configuration package writes validated updates atomically and exposes an
in-process `Store` for serialized updates, so the future local API can safely
use the same file as its persistent state.

Only proxies with `active: true` are started. `active` defaults to `true`; set
`active: false` to stage a proxy without binding its listener.

## Development

```sh
go run ./cmd/ctf-proxy
```

The initial executable only confirms that the project is wired correctly; proxy
behaviour will be introduced incrementally.
