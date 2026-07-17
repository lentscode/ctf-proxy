# ctf-proxy — Project Guide

## Purpose

Build a custom layer-4 proxy for Attack & Defense CTF vulnerability boxes, written
primarily in Go. It is an operator-controlled traffic mediation tool, not a generic
internet-facing reverse proxy.

## Required capabilities

- Intercept TCP and HTTP traffic destined for protected services. The proxy must bind
  the port formerly used by a service and forward traffic to that service after it has
  been moved to a private upstream port/address.
- Filter both directions independently:
  - request/client-to-service traffic;
  - response/service-to-client traffic.
- A filter decision must be able to allow or reject traffic. Rejection behavior must
  be protocol-aware where possible (for example, an HTTP status response) and safe
  for raw TCP (normally closing the connection).
- Support three filter sources:
  - declarative YAML rules;
  - a maintained set of built-in filters;
  - optional Go plugins, with a stable, versioned interface and strict isolation of
    plugin failures from the proxy process.
- Provide a web dashboard for managing active proxies on the vulnbox, adding and
  removing filters, and observing traffic and filter decisions.
- Make deployment simple and fast, ideally a single statically linked Go binary plus
  configuration. Avoid requiring a database for the first usable version.
- Prioritize low latency, low allocation rate, bounded memory use, and low CPU
  overhead. The data plane must remain useful even when the dashboard/control plane
  is unavailable.

## Architecture boundaries

Keep the project split conceptually into:

1. **Data plane** — listeners, TCP/HTTP forwarding, stream handling, deadlines,
   metrics emission, and filter execution. It must not block on the dashboard.
2. **Filter engine** — protocol-neutral decision types plus adapters for TCP and
   HTTP. Configuration validation occurs before a changed filter set is activated.
3. **Control plane** — local authenticated HTTP API used by the dashboard to manage
   proxy definitions and filter configuration.
4. **Dashboard** — a static browser client served by the Go binary. It consumes the
   local control-plane API and should not contain security-critical policy logic.

For the initial milestone, prefer explicit configuration and a small set of built-in
filters over a broad plugin system. Go's native `plugin` package has deployment and
platform constraints; treat runtime plugins as a later design decision, potentially
implemented as separately supervised processes or precompiled extensions.

## Safety and operational rules

- Do not accidentally expose an upstream service on both its old and new ports.
- Bind the management API to localhost by default and require authentication before
  exposing it beyond the vulnbox.
- Treat configurations and plugin code as trusted operator inputs, but validate all
  configuration before applying it.
- Never log complete credentials, flags, authorization headers, cookies, or full
  sensitive request bodies by default. Provide deliberate, bounded observability.
- Bound connection counts, buffered bytes, log queues, and traffic-retention memory.
- Ensure one bad filter, connection, or dashboard request cannot stop unrelated
  proxied connections.

## Development approach

- Write small packages with unit tests before adding dashboard features.
- Start with raw TCP pass-through, then HTTP-aware forwarding, then YAML filters,
  built-ins, dashboard/API, and finally an optional plugin strategy.
- Benchmark the forwarding path and use Go's race detector and profiling before
  optimizing based on intuition.
- Keep external dependencies minimal and justify each one.
- Do not implement large features unless specifically requested by the user. Act as
  a teacher: explain issues in user-authored code, identify trade-offs, and suggest
  incremental improvements rather than silently building the whole project.

## Initial repository layout

```text
cmd/ctf-proxy/       executable entrypoint
internal/proxy/      forwarding/data-plane implementation
internal/filter/     filter contracts and implementations
internal/control/    management API and configuration lifecycle
web/                 static dashboard assets (HTML, CSS, TypeScript)
```

