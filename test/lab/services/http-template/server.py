from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs


def fake_flag(label: str) -> str:
    return (label.upper().replace("_", "") + "0" * 35)[:35] + "="


class Handler(BaseHTTPRequestHandler):
    def log_message(self, format, *args):
        pass

    def send_html(self, status: int, text: str) -> None:
        body = text.encode()
        self.send_response(status)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:
        if self.path == "/healthz":
            self.send_html(200, "healthy")
        elif self.path == "/":
            self.send_html(200, '<form method="post" action="/render"><textarea name="template">{{title}}</textarea><button>Render</button></form>')
        else:
            self.send_html(404, "not found")

    def do_POST(self) -> None:
        if self.path != "/render":
            self.send_html(404, "not found")
            return
        length = int(self.headers.get("Content-Length", "0"))
        template = parse_qs(self.rfile.read(length).decode("utf-8", "replace"), keep_blank_values=True).get("template", [""])[0]
        if not template:
            self.send_html(400, "template is required")
            return
        # Deliberately vulnerable demonstration of server-side template context exposure.
        rendered = template.replace("{{title}}", "CTF report").replace("{{flag}}", fake_flag("http2_template"))
        self.send_html(200, f"<pre>{escape(rendered)}</pre>")


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
