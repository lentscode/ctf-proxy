import base64
import json
import socketserver


def fake_flag(label: str) -> str:
    return (label.upper().replace("_", "") + "0" * 35)[:35] + "="


def reply(payload: dict) -> bytes:
    return base64.b64encode(json.dumps(payload, separators=(",", ":")).encode()) + b"\n"


class Handler(socketserver.StreamRequestHandler):
    def handle(self) -> None:
        raw = self.rfile.readline(65537).strip()
        try:
            if not raw:
                raise ValueError("empty request")
            decoded = base64.b64decode(raw, validate=True)
            payload = json.loads(decoded)
            if not isinstance(payload, dict):
                raise ValueError("JSON payload must be an object")
            if not isinstance(payload.get("message"), str):
                raise ValueError("message is required")
        except (ValueError, json.JSONDecodeError, UnicodeDecodeError) as error:
            self.wfile.write(reply({"ok": False, "error": str(error)}))
            return

        result = {"ok": True, "message": payload["message"]}
        if payload.get("admin") is True:
            result["flag"] = fake_flag("tcp1_admin")
        self.wfile.write(reply(result))


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True


if __name__ == "__main__":
    with Server(("0.0.0.0", 9000), Handler) as server:
        server.serve_forever()
