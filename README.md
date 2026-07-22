# ctf-proxy

A learning-oriented, low-overhead layer-4 proxy for Attack & Defense CTF
vulnerability boxes.

The project will provide TCP and HTTP forwarding with bidirectional filtering and a
local web dashboard. See [agents.md](agents.md) for the agreed goals, constraints,
and intended architecture.

## Configuration

`ctf-proxy` reads `./ctf-proxy.yaml` by default. Set `CTF_PROXY_CONFIG` to
choose a different file. Relative filter paths are resolved relative to the
main configuration file. If the file is absent, the process creates a valid
empty configuration (`version: 1`, `proxies: []`) and starts the local control
API, so proxies can be added after startup.

```yaml
version: 1
max_connections: 128 # per proxy; omit to use the default
filter_files: # loaded once; YAML filter names are available to every proxy
  - filters/common.yaml
proxies:
  - name: web
    active: true
    protocol: http
    listen: ":8080"
    upstream: "http://127.0.0.1:18080"
    filters:
      - reject-debug-path # YAML filter from filters/common.yaml
      # - block-admin # compiled Go filter, once registered in internal/filter/builtin.go

  - name: challenge
    active: false # staged in configuration, but not started
    protocol: tcp
    listen: ":31337"
    upstream: "127.0.0.1:31338"
    filters:
      - reject-debug-path
```

The configuration is strictly validated before any proxy is started. The
configuration package writes validated updates atomically and exposes an
in-process `Store` for serialized updates, so the future local API can safely
use the same file as its persistent state.

YAML filters are loaded once from the top-level `filter_files` list. Compiled Go
filters are registered in `internal/filter/builtin.go`. Both kinds share one
global namespace; each proxy selects an ordered subset using `filters`. Filter
names must therefore be unique across all YAML files and compiled Go filters.

Only proxies with `active: true` are started. `active` defaults to `true`; set
`active: false` to stage a proxy without binding its listener. An empty proxy
list is valid.

## Local control API

The binary serves an authenticated management API at `127.0.0.1:8081` by
default. On its first startup it creates a bearer token in `.tokens` (mode
`0600`) and prints it once to stderr. Use `CTF_PROXY_CONTROL_ADDR` to select
another **loopback-only** address; non-loopback listeners are rejected.

The API creates, replaces, activates, deactivates, and removes only the
affected proxy listener; it does not restart unrelated proxies. It persists
accepted changes atomically to the main configuration file.

```sh
# Inspect health and configured proxies.
curl -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/healthz
curl -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/api/v1/proxies

# Add a TCP proxy.
curl -X POST http://127.0.0.1:8081/api/v1/proxies \
  -H "Authorization: Bearer $CTF_PROXY_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"challenge","protocol":"tcp","listen":":31337","upstream":"127.0.0.1:31338","filters":[]}'

# Stage, activate, or remove a proxy.
curl -X POST -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/api/v1/proxies/challenge/deactivate
curl -X POST -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/api/v1/proxies/challenge/activate
curl -X DELETE -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/api/v1/proxies/challenge

# List the configured built-in and YAML filter names available to proxies.
curl -H "Authorization: Bearer $CTF_PROXY_TOKEN" http://127.0.0.1:8081/api/v1/filters
```

This MVP only attaches existing filter names to proxies. YAML filter files are
loaded and validated at process startup; authoring or reloading filter files
through the API is intentionally deferred.

## Observability

Warnings, errors, and filter rejections are emitted as structured JSON to
stderr and kept in a shared, in-memory event history for the local dashboard.
The history holds the newest 512 events, is never consumed by a reader, and is
lost on process restart. Events deliberately exclude traffic payloads, HTTP
headers, URLs, peer addresses, and credentials.

```sh
# Fetch the most recent events (default: 100; maximum: 512).
curl -H "Authorization: Bearer $CTF_PROXY_TOKEN" \
  'http://127.0.0.1:8081/api/v1/events?limit=100'

# Subscribe to new events using Server-Sent Events.
curl -N -H "Authorization: Bearer $CTF_PROXY_TOKEN" \
  http://127.0.0.1:8081/api/v1/events/stream
```

At most 16 live stream clients are supported. A client that cannot keep up is
disconnected; its browser should reconnect and fetch a fresh snapshot. Event
production never waits for dashboard clients or stderr writes.

## Development

```sh
go run ./cmd/ctf-proxy
```

Run the test suite with:

```sh
go test ./...
go test -race ./...
```
