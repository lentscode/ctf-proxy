# ctf-proxy

A learning-oriented, low-overhead layer-4 proxy for Attack & Defense CTF
vulnerability boxes.

The project will provide TCP and HTTP forwarding with bidirectional filtering and a
local web dashboard. See [agents.md](agents.md) for the agreed goals, constraints,
and intended architecture.

## Development

```sh
go run ./cmd/ctf-proxy
```

The initial executable only confirms that the project is wired correctly; proxy
behaviour will be introduced incrementally.

