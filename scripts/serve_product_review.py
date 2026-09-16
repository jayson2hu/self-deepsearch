"""Serve only the generated, standalone product review; never the repository."""

import argparse
import base64
import hmac
import json
from functools import partial
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


class ReviewHandler(BaseHTTPRequestHandler):
    def __init__(self, *args, review_file: Path, authorization: str, **kwargs):
        self.review_file = review_file
        self.authorization = authorization
        super().__init__(*args, **kwargs)

    def do_HEAD(self):
        self.respond(head=True)

    def do_GET(self):
        self.respond(head=False)

    def respond(self, *, head):
        route = urlsplit(self.path).path
        if route != "/healthz" and not hmac.compare_digest(
            self.headers.get("Authorization", "").encode("utf-8"),
            self.authorization.encode("utf-8"),
        ):
            body = b"Authentication required.\n"
            self.send_response(401)
            self.send_header("WWW-Authenticate", 'Basic realm="Mujian product review", charset="UTF-8"')
            self.send_header("Content-Type", "text/plain; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Robots-Tag", "noindex, nofollow, noarchive")
            self.end_headers()
            if not head:
                self.wfile.write(body)
            return
        if route == "/healthz":
            body, content_type = b"ok\n", "text/plain; charset=utf-8"
        elif route == "/robots.txt":
            body, content_type = b"User-agent: *\nDisallow: /\n", "text/plain; charset=utf-8"
        elif route in ("/", "/index.html", "/download"):
            try:
                body = self.review_file.read_bytes()
            except OSError:
                self.send_error(503, "Review is temporarily unavailable")
                return
            content_type = "text/html; charset=utf-8"
        else:
            self.send_error(404, "Not found")
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-cache")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("X-Robots-Tag", "noindex, nofollow, noarchive")
        self.send_header("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
        if route == "/download":
            self.send_header("Content-Disposition", 'attachment; filename="mujian-product-review.html"')
        self.end_headers()
        if not head:
            self.wfile.write(body)

    def log_message(self, *_args):
        # This review does not collect visit paths, IPs, or review notes.
        pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=13002)
    parser.add_argument("--auth-file", type=Path, required=True,
                        help="Private JSON file containing username and password")
    args = parser.parse_args()
    review_file = Path(__file__).resolve().parents[1] / "docs/product-review/index.html"
    if not review_file.is_file():
        parser.error("Build the page first: node scripts/build_product_review.mjs")
    credentials = json.loads(args.auth_file.read_text(encoding="utf-8"))
    username, password = credentials.get("username", ""), credentials.get("password", "")
    if not isinstance(username, str) or not isinstance(password, str) or not username or ":" in username or len(password) < 20:
        parser.error("Require a username and a randomly generated password of at least 20 characters")
    authorization = "Basic " + base64.b64encode(f"{username}:{password}".encode("utf-8")).decode("ascii")
    server = ThreadingHTTPServer((args.host, args.port), partial(
        ReviewHandler, review_file=review_file, authorization=authorization))
    print(f"Product review listening on {args.host}:{args.port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
