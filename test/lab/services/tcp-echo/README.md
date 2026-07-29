# TCP echo lab service

This intentionally vulnerable fixture listens on TCP port `9000`. It accepts
one newline-delimited Base64-encoded JSON request per connection.

## Behaviour

- `message` is required and must be a string; it is returned in the response.
- `admin: true` deliberately includes a pattern-valid fake flag.
- Invalid Base64, JSON, or payload shape produces a Base64-encoded JSON error.

Start it independently with:

```sh
docker compose up --build
```

Exercise the normal and vulnerable paths with:

```sh
python3 client.py --host 127.0.0.1 --port 24101 --message hello
python3 client.py --host 127.0.0.1 --port 24101 --admin
```

The interactive lab assigns a temporary published port; use the port it prints
instead of `24101`. This service exists only as a disposable lab fixture.
