"""Serve only the design review bundle, on loopback by default; never the repository."""

import argparse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[1] / "docs/design-review-2026-10"
FILES = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/index.html": ("index.html", "text/html; charset=utf-8"),
    "/preview.html": ("preview.html", "text/html; charset=utf-8"),
    **{f"/{name}.css": (f"{name}.css", "text/css; charset=utf-8") for name in ("review", "preview")},
    **{f"/{name}.mjs": (f"{name}.mjs", "text/javascript; charset=utf-8") for name in ("data", "review", "review-store", "preview")},
    **{f"/thumbs/{number:02d}-{view}.jpg": (f"thumbs/{number:02d}-{view}.jpg", "image/jpeg") for number in range(1, 21) for view in ("public", "ops", "mobile")},
}
CSP = "; ".join((
    "default-src 'none'", "script-src 'self'", "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:", "connect-src 'none'", "frame-src 'self'",
    "frame-ancestors 'self'", "base-uri 'none'", "object-src 'none'", "form-action 'none'",
))


class Handler(BaseHTTPRequestHandler):
    def do_HEAD(self):
        self.respond(head=True)

    def do_GET(self):
        self.respond(head=False)

    def respond(self, *, head):
        route = urlsplit(self.path).path
        status = 200
        if route == "/healthz":
            body, content_type = b"ok\n", "text/plain; charset=utf-8"
        elif route == "/robots.txt":
            body, content_type = b"User-agent: *\nDisallow: /\n", "text/plain; charset=utf-8"
        elif route in FILES:
            filename, content_type = FILES[route]
            target = ROOT / filename
            # Fixed allowlist plus resolved containment: no encoded traversal or symlink escape.
            if not target.resolve().is_relative_to(ROOT.resolve()) or not target.is_file():
                status, body, content_type = 404, b"Not found\n", "text/plain; charset=utf-8"
            else:
                body = target.read_bytes()
        else:
            status, body, content_type = 404, b"Not found\n", "text/plain; charset=utf-8"
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Robots-Tag", "noindex, nofollow, noarchive")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("Content-Security-Policy", CSP)
        self.send_header("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
        self.end_headers()
        if not head:
            self.wfile.write(body)

    def log_message(self, *_args):
        pass


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=13004)
    # Deliberately no --host flag: external publication requires a separate reviewed deployment.
    args = parser.parse_args()
    if not 0 <= args.port <= 65535:
        parser.error("Port must be between 0 and 65535")
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    print(f"Design review: http://127.0.0.1:{server.server_port}/", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
