from html import escape
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs


def fake_flag(label: str) -> str:
    return (label.upper().replace("_", "") + "0" * 35)[:35] + "="


USERS = {"alice": "wonderland", "bob": "builder"}


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
            self.send_html(200, '<form method="post" action="/login"><input name="username"><input name="password" type="password"><button>Login</button></form>')
        else:
            self.send_html(404, "not found")

    def do_POST(self) -> None:
        if self.path != "/login":
            self.send_html(404, "not found")
            return
        length = int(self.headers.get("Content-Length", "0"))
        values = parse_qs(self.rfile.read(length).decode("utf-8", "replace"), keep_blank_values=True)
        username = values.get("username", [""])[0]
        password_given = "password" in values and values["password"][0] != ""
        password = values.get("password", [""])[0]
        if not username:
            self.send_html(400, "username is required")
        elif password_given:
            if USERS.get(username) == password:
                self.send_html(200, f"welcome {escape(username)}")
            else:
                self.send_html(401, "invalid credentials")
        elif username == "admin":
            self.send_html(200, f"welcome admin {fake_flag('http1_login')}")
        else:
            self.send_html(400, "password is required")


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
