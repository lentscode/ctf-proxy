# HTTP template lab service

This intentionally vulnerable fixture listens on HTTP port `8081`. It serves a
simple form at `/` and renders submitted values at `POST /render`.

## Behaviour

- `{{title}}` is replaced with a harmless report title.
- `{{flag}}` is deliberately substituted from a server-side context and
  returns a pattern-valid fake flag.
- Missing templates return `400`; `GET /healthz` is harmless.

Start it independently with:

```sh
docker compose up --build
```

Exercise it with:

```sh
python3 client.py --host 127.0.0.1 --port 24104
python3 client.py --host 127.0.0.1 --port 24104 --exploit
```

The interactive lab assigns a temporary published port; use the port it prints
instead of `24104`. This service exists only as a disposable lab fixture.
