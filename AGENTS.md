# ctf-proxy — Project Guide

## Purpose

`ctf-proxy` is an operator-controlled TCP and HTTP mediation proxy for Attack
& Defense CTF vulnerability boxes. It takes over a service's public port and
forwards to that service on a private upstream address, applying independently
configured request and response filters. It is deliberately not a general
internet-facing reverse proxy.

The project is a Go control/data-plane binary with a local browser dashboard.
Configuration is file-backed YAML; the first usable deployment must not require
a database.

## Current state

The core proxy is implemented and covered by unit and integration tests:

- TCP forwarding supports simultaneous bidirectional copies, half-closes, a
  bounded per-proxy connection limit, and safe connection closure on rejection.
- HTTP forwarding supports `http` and `https` upstreams, strips hop-by-hop
  headers, uses bounded transport/server timeouts, limits concurrent requests,
  and returns protocol-aware `403` responses for rejected traffic.
- The filter engine has immutable ordered chains, a stable Go contract,
  failure isolation (errors, invalid decisions, and panics fail open), YAML
  rule compilation, and a registry reserved for compiled-in filters.
- YAML rules currently support `tcp.body`, `http.path`, `http.header`, and
  `http.body`, with exact/contains/prefix/suffix/regular-expression matching.
  Rules are reject-only; filtering is direction- and protocol-specific.
- HTTP bodies are only buffered when a selected filter needs them. Inspection
  is capped at 64 KiB; oversize bodies remain intact for forwarding but are
  reported to filters as unavailable.
- The localhost-only, bearer-token-protected control API manages proxy
  definitions, activation, and the filter catalogue. Valid changes are started
  and persisted atomically, with listener conflicts and failed changes rolled
  back where possible.
- Observability is bounded and sanitized: structured stderr logging, a
  512-event in-memory history, and a capacity-limited SSE stream. Traffic,
  credentials, headers, URLs, peer addresses, and bodies are not exposed in
  events.

The frontend is only a React/Vite starter scaffold. It is not connected to the
API, built into the Go binary, or served by it. `web/` is legacy/planned
documentation rather than the active frontend location.

## Repository layout

```text
cmd/ctf-proxy/              Go executable: startup, signal shutdown, tokens,
                            control listener
internal/config/            Versioned YAML config, strict validation, atomic
                            file persistence, in-process Store
internal/proxy/             TCP and HTTP data-plane runners and integration tests
internal/filter/            Filter contract, immutable chains, registry, YAML
                            compiler, built-in registration point
internal/control/           Lifecycle Manager, bearer authentication, local API,
                            SSE event endpoints
internal/observe/           Sanitized event model, bounded fan-out/history, JSON
                            logging adapter
frontend/                   React + TypeScript + Vite dashboard source (scaffold)
public/                     Vite static assets
web/                        Legacy dashboard note; do not add new application code
                            here without first consolidating the frontend layout
```

## Runtime model and interfaces

The executable uses these environment variables:

- `CTF_PROXY_CONFIG`: main configuration path (default `ctf-proxy.yaml`). A
  missing file is created as an empty version-1 configuration.
- `CTF_PROXY_CONTROL_ADDR`: control API listener (default `127.0.0.1:8081`).
  The binary refuses a non-loopback address.
- `CTF_PROXY_TOKENS_FILE`: bearer-token file (default `.tokens`). If missing
  or empty, a token is generated and written with restrictive permissions.

The version-1 configuration contains `max_connections`, global `filter_files`,
and `proxies`. A proxy has a unique `name`, `active` (default true), `protocol`
(`tcp` or `http`), public `listen` address, private `upstream`, and an ordered
list of filter names. TCP upstreams are `host:port`; HTTP upstreams are bare
`http://host[:port]` or `https://host[:port]` URLs.

The control API is authenticated on every endpoint:

```text
GET    /healthz
GET    /api/v1/proxies
POST   /api/v1/proxies
GET    /api/v1/proxies/{name}
PUT    /api/v1/proxies/{name}
DELETE /api/v1/proxies/{name}
POST   /api/v1/proxies/{name}/activate
POST   /api/v1/proxies/{name}/deactivate
GET    /api/v1/filters
GET    /api/v1/events?limit=1..512
GET    /api/v1/events/stream
```

## Architecture boundaries

1. **Data plane (`internal/proxy`)** owns listeners, forwarding, deadlines,
   connection slots, filter invocation, and proxy failure events. It must never
   wait for the dashboard, SSE clients, or file persistence.
2. **Filter engine (`internal/filter`)** owns protocol-neutral messages,
   decisions, validation, matching, and filter isolation. Keep it independent
   of HTTP handlers, configuration storage, and UI code.
3. **Control plane (`internal/control`, `internal/config`)** owns lifecycle
   reconciliation and durable operator configuration. Validate a complete next
   configuration and construct its runners before replacing the active config.
4. **Observability (`internal/observe`)** owns safe, bounded event delivery.
   Add only metadata that remains safe to expose to a local operator.
5. **Dashboard (`frontend`)** is an untrusted, client-side-rendered
   presentation layer. It consumes the local API and must not contain policy or
   bypass server-side validation.

## Frontend directives

- Build a clean, simple, operational UI. Favor a small number of focused views,
  clear proxy status, readable event history, and straightforward forms over
  decorative graphics, dense dashboards, or elaborate interactions.
- Support a dark theme only. Use a deliberate, accessible dark palette with
  sufficient contrast for status, error, focus, and disabled states; do not add
  a light-theme toggle or system-theme switching.
- Use client-side rendering only. The Vite build is a static application that
  calls the authenticated local API from the browser; do not introduce SSR,
  server components, or an additional frontend runtime.

## What remains to build

Work incrementally; do not begin a large feature without a focused request.

1. **Turn the frontend scaffold into the dashboard.** Replace template assets
   with an authenticated proxy-management UI: list/create/edit/delete/activate
   proxies, show available filters, show recent events, and subscribe to SSE.
   Follow the frontend directives above; keep the bearer token in deliberate
   local session state, validate API payloads (Zod is already installed), and
   never render raw event data as HTML.
2. **Ship the dashboard with the binary.** Make the Vite production build an
   explicit build dependency, embed its output with `embed.FS`, and serve it
   from the Go process without shadowing `/api/` or `/healthz`. Add an end-to-end
   test for asset serving and client-side route fallback if routing is added.
3. **Document and operationalize configuration.** Replace the root Vite README
   with project documentation, add minimal example main/filter YAML files, and
   document moving upstream services to private ports before enabling listeners.
   Add a reproducible static-release build target and deployment instructions.
4. **Add useful built-in filters.** Implement each small filter in
   `internal/filter`, register it in `RegisterBuiltins`, define its requirements
   precisely, and test acceptance, rejection, bad input, and concurrent use.
   Start with narrowly scoped CTF protections rather than generic rule systems.
5. **Harden the data plane from measured needs.** Make TCP deadlines and HTTP
   transport/server limits configurable with validated bounds, then benchmark
   the forwarding path and run race tests. Preserve the current non-blocking,
   bounded-observability guarantees.
6. **Improve lifecycle resilience.** Decide and document the desired behavior
   for an unexpectedly stopped runner (status only, bounded retry, or explicit
   operator restart), then test it. Add metrics only if they remain bounded and
   do not couple the data plane to control-plane availability.
7. **Defer runtime plugins.** Do not use Go's native `plugin` package for the
   initial releases. If third-party filters become necessary, design a
   versioned, supervised out-of-process protocol with timeouts, memory/CPU
   limits, and fail-open isolation before implementing it.

## Safety and development rules

- Never leave an upstream service exposed on both its original public port and
  its private upstream port. Validate the intended bind/upstream topology in
  deployment documentation and tests.
- Keep the management API loopback-bound by default. Any future remote access
  requires explicit authentication and transport-security design; do not loosen
  `ListenLoopback` casually.
- Treat config and filter definitions as trusted operator input, but retain
  strict parsing and complete validation before activation.
- Do not log or publish flags, credentials, authorization headers, cookies,
  URLs, peer addresses, or full bodies. Preserve bounded queues, buffers,
  connection slots, event history, and subscriber counts.
- A bad filter must not stop unrelated connections. Keep filter failures
  isolated and fail open unless an explicitly approved policy changes that
  contract.
- Add focused tests next to code. For forwarding/control changes run
  `go test ./...`; use `go test -race ./...` before accepting concurrency
  changes. For frontend changes run `pnpm lint` and `pnpm build`.
- Keep dependencies minimal. Prefer the standard library and explain any new
  runtime dependency in documentation or the change description.
