import json
import posixpath
import socketserver


def fake_flag(label: str) -> str:
    return (label.upper().replace("_", "") + "0" * 35)[:35] + "="


FILES = {"public/welcome.txt": "Welcome to the archive.", "public/rules.txt": "Keep paths inside public.", "flag.txt": fake_flag("tcp2_archive")}


class Handler(socketserver.StreamRequestHandler):
    def handle(self) -> None:
        try:
            payload = json.loads(self.rfile.readline(65537))
            path = payload.get("path") if isinstance(payload, dict) else None
            if not isinstance(path, str):
                raise ValueError("path is required")
            # Deliberately vulnerable: normalization can escape public/.
            # Public paths are accepted in their displayed form as well as
            # relative to public/, so normal reads remain easy to demonstrate.
            resolved = posixpath.normpath(path if path.startswith("public/") else posixpath.join("public", path))
            content = FILES.get(resolved)
            if content is None:
                raise ValueError("file not found")
            result = {"ok": True, "content": content}
        except (ValueError, json.JSONDecodeError) as error:
            result = {"ok": False, "error": str(error)}
        self.wfile.write(json.dumps(result, separators=(",", ":")).encode() + b"\n")


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True


if __name__ == "__main__":
    with Server(("0.0.0.0", 9001), Handler) as server:
        server.serve_forever()
