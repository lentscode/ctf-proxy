# TCP archive lab service

This intentionally vulnerable fixture listens on TCP port `9001` and accepts
newline-delimited JSON requests of the form `{"path":"..."}`.

## Behaviour

- `public/welcome.txt` and `public/rules.txt` are normal readable files.
- The path is normalized without verifying that it remains below `public/`.
- `../flag.txt` deliberately escapes the public archive and returns a
  pattern-valid fake flag.

Start it independently with:

```sh
docker compose up --build
```

Exercise it with:

```sh
python3 client.py --host 127.0.0.1 --port 24102
python3 client.py --host 127.0.0.1 --port 24102 --exploit
```

The interactive lab assigns a temporary published port; use the port it prints
instead of `24102`. This service exists only as a disposable lab fixture.
