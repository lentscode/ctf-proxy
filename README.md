# ctf-proxy

`ctf-proxy` is an operator-controlled TCP and HTTP mediation proxy for Attack
& Defense CTF vulnboxes. Its local dashboard manages proxy definitions,
filters, recent events, and the optional Scan and configure workflow.

## Scan and configure (AD CTF only)

The **Scan and configure** control on the Proxies page discovers `docker-compose.yaml`,
`docker-compose.yml`, `compose.yaml`, and `compose.yml` in the immediate
subdirectories of `CTF_PROXY_COMPOSE_ROOT` (default `/root`). It never scans
or changes Docker state at startup. Add further filename variants with
`CTF_PROXY_COMPOSE_FILE_NAMES`, using a comma-separated list; those names are
added to the standard set. Names containing directory components are ignored.

After an authenticated operator scan and confirmation, selected explicit TCP
port publications are moved to an unused `127.0.0.1` port in the 20000–59999
range. The original public binding is assigned to a generated ctf-proxy
listener, then only the affected Compose services are recreated with Docker
Compose v2 (`docker compose`). This keeps the upstream private while retaining
the original public service port.

The first version supports explicit single TCP port mappings in short or long
Compose syntax. It reports but does not manage UDP, ranges, dynamic ports,
loopback-only mappings, host networking, and unsupported YAML forms. For every
eligible port, select TCP or HTTP in the review. HTTP also requires choosing
`http` or `https` for its upstream scheme.

Use **Restore** on the same page to remove generated proxies and recreate the
affected service with its exact original Compose file. Restoration refuses to
overwrite a Compose file that changed after configuration; resolve that drift before
trying again. Private restore records live beside the main config in
`.ctf-proxy-state` and are never exposed through the API.

`docker compose` must be installed and usable by the account running
ctf-proxy. Do not leave a service published on its original port outside this
workflow while a matching proxy is active.

## Development

Run the dashboard and local API with `pnpm dev`. Release builds use:

```sh
pnpm build
```

Run checks with:

```sh
go test ./...
go test -race ./...
pnpm lint
pnpm build
```

## Real-world Docker lab

The opt-in lab starts four intentionally vulnerable Python fixture services,
uses the real dashboard to take them over with Docker Compose, and verifies
real TCP and HTTP traffic through the release binary. It is intended for local
demonstrations and isolated nightly CI runners; never point it at a shared
Docker host or a real Compose root.

Prerequisites are Docker Compose v2, Python 3, pnpm, and Playwright Chromium:

```sh
pnpm exec playwright install chromium
pnpm test:lab
```

Successful runs remove their temporary service root and Docker projects. Use
`pnpm test:lab:keep` to preserve the temporary artifacts for diagnosis.

### Interactive lab

To inspect the real dashboard and traffic while the lab is running, use:

```sh
pnpm lab:up
```

The command prints a dashboard URL, control token, fixture ports, and Python
client commands. The services begin directly exposed so the intended
vulnerabilities can be observed. Use **Proxies → Scan and configure** to move
them behind ctf-proxy. The predefined `lab-*` YAML filters are available in
each proxy editor's **Available filters** list; attach the relevant rules and
watch the Events panel. Press
`Ctrl-C` to stop the proxy and tear down only the disposable lab containers.
Use `pnpm lab:up:keep` to retain the generated configuration, staged Compose
files, and logs after shutdown.
