# HTTP login lab service

This intentionally vulnerable fixture listens on HTTP port `8080` and exposes
a form at `/` plus `POST /login`.

## Behaviour

- Valid in-memory credentials are `alice` / `wonderland` and `bob` / `builder`.
- A supplied username and password are checked strictly against that map.
- `username=admin` with no password deliberately returns a pattern-valid fake
  flag.
- A non-admin username without a password is rejected.
- `GET /healthz` is a harmless readiness endpoint.

Start it independently with:

```sh
docker compose up --build
```

Exercise it with:

```sh
python3 client.py --host 127.0.0.1 --port 24103 --username alice --password wonderland
python3 client.py --host 127.0.0.1 --port 24103 --admin-exploit
```

The interactive lab assigns a temporary published port; use the port it prints
instead of `24103`. This service exists only as a disposable lab fixture.
