# TCP echo lab service

This intentionally vulnerable fixture accepts a Base64-encoded JSON line. A
request with `{"message":"...","admin":true}` returns a fake flag.
