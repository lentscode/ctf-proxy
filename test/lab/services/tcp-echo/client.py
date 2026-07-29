import argparse
import base64
import json
import re
import socket
import sys

FLAG = re.compile(r"^[A-Z0-9]{35}=$")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--message", default="hello")
    parser.add_argument("--admin", action="store_true")
    parser.add_argument("--malformed", action="store_true")
    args = parser.parse_args()
    request = b"not-base64\n" if args.malformed else base64.b64encode(json.dumps({"message": args.message, "admin": args.admin}).encode()) + b"\n"
    try:
        with socket.create_connection((args.host, args.port), timeout=3) as conn:
            conn.sendall(request)
            response = conn.makefile("rb").readline(65537).strip()
        payload = json.loads(base64.b64decode(response, validate=True))
        flag = payload.get("flag")
        print(json.dumps({"ok": payload.get("ok", False), "echoed": payload.get("message") == args.message, "flag_found": isinstance(flag, str), "flag_format_valid": isinstance(flag, str) and bool(FLAG.fullmatch(flag)), "error": payload.get("error", "")}))
    except Exception as error:
        print(json.dumps({"ok": False, "echoed": False, "flag_found": False, "flag_format_valid": False, "connection_error": type(error).__name__}))


if __name__ == "__main__":
    main()
