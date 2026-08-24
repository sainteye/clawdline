#!/usr/bin/env python3
"""Serve the picture-taking pages on one origin with the real server behind them.

This exists for the notification picture, and for one reason: **a browser will only accept a
push subscription for the origin the page came from**, and it will only run a service worker
served from that origin. `tools/phone/index.html` draws a phone; the notification inside it has to be
a real one, minted by the real VAPID key and delivered by the real push service. Those two facts
put the frame and the API on the same origin or nowhere.

So this is a two-line web server: files out of a directory, and everything under `/v1/` and the
app's own icons passed straight through to Clawdline with the machine's own token attached —
the one at `~/.config/clawdline/remote-token` that `docs/api.md` is about, which exists precisely
so that a script running as you does not have to invent credentials.

    ./tools/web-serve.py --root tools/phone --port 7788

It has since acquired a second job, for a related reason: the page's styles are a dozen separate
files and a browser will not fetch a `<link>` from a `file://` origin, so the copy of the web
interface that the README's pictures are taken from — and that mock mode is developed against —
needs an origin too.

    ./tools/web-serve.py --root Resources/web --port 7789   # then open /?mock=1&write=1

Pointing `--upstream` at a port nothing listens on turns it back into a plain file server, which is
what `tools/shoot-assets.sh` does for those two pictures: nothing behind it means nothing of yours
can appear in them.

**Nothing it serves is a stand-in for the server.** `/v1/push/key`, `/v1/push/subscribe` and
`/v1/push/test` are answered by Clawdline, so a notification that arrives arrived the whole way.
The only thing this adds is the header, and the only thing it hides is the token.

It binds loopback, like the thing behind it.
"""

import argparse
import http.server
import json
import os
import sys
import urllib.error
import urllib.request

PASS_PREFIXES = ("/v1/", "/icon-", "/splash-")
PASS_EXACT = ("/favicon.ico", "/manifest.webmanifest")


class Handler(http.server.SimpleHTTPRequestHandler):
    # Said here rather than left to the system's table. `mimetypes` reads `/etc/apache2/mime.types`
    # and friends, so what a `.js` is called depends on the machine, and `SimpleHTTPRequestHandler`
    # falls back to `application/octet-stream` when it does not know — which a browser refuses to
    # run as a module, silently enough that the console never says MIME. The page is served as
    # several dozen modules and stylesheets now, so this is not a corner.
    extensions_map = {**http.server.SimpleHTTPRequestHandler.extensions_map,
                      ".js": "text/javascript", ".mjs": "text/javascript", ".css": "text/css"}
    upstream = "127.0.0.1:7717"
    token = ""

    def log_message(self, fmt, *args):          # quiet: the shoot's own log is the interesting one
        pass

    def _proxied(self):
        return self.path.startswith(PASS_PREFIXES) or self.path.split("?")[0] in PASS_EXACT

    def _relay(self, body=None):
        url = "http://%s%s" % (self.upstream, self.path)
        request = urllib.request.Request(url, data=body, method=self.command)
        if self.token:
            request.add_header("Authorization", "Bearer " + self.token)
        ctype = self.headers.get("content-type")
        if ctype:
            request.add_header("Content-Type", ctype)
        key = self.headers.get("idempotency-key")
        if key:
            request.add_header("Idempotency-Key", key)
        # The `Origin` the page sends names this server's port, and the port is not what the
        # check is about — Clawdline compares the *host*. Sent as loopback so the two ends agree
        # about what "our own page" means whichever port this happens to have taken.
        request.add_header("Origin", "http://127.0.0.1")
        # Deliberately not forwarded: `Sec-Fetch-*`. Those headers describe a browser's
        # relationship to *this* server, and repeating them to another one would be describing a
        # request that never happened. Absent, they mean "not a browser", and the token answers
        # for it — which is exactly what this is.
        try:
            with urllib.request.urlopen(request, timeout=20) as answer:
                data = answer.read()
                self.send_response(answer.status)
                for name in ("Content-Type", "Cache-Control"):
                    if answer.headers.get(name):
                        self.send_header(name, answer.headers[name])
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        except urllib.error.HTTPError as e:
            data = e.read()
            self.send_response(e.code)
            self.send_header("Content-Type", e.headers.get("Content-Type", "application/json"))
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:                                       # the app is not running
            data = json.dumps({"error": {"code": "upstream", "message": str(e)}}).encode()
            self.send_response(502)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)

    def do_GET(self):
        if self._proxied():
            return self._relay()
        return super().do_GET()

    def do_POST(self):
        if not self._proxied():
            self.send_error(404)
            return
        length = int(self.headers.get("content-length") or 0)
        return self._relay(self.rfile.read(length) if length else b"")

    def end_headers(self):
        # A service worker is only allowed to control what it is scoped to, and a stale one is
        # the hardest thing in this file to debug: the page reloads, the code does not.
        self.send_header("Cache-Control", "no-store")
        self.send_header("Service-Worker-Allowed", "/")
        super().end_headers()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default="tools/phone")
    ap.add_argument("--port", type=int, default=7788)
    ap.add_argument("--upstream", default=None, help="host:port of the Clawdline server")
    ap.add_argument("--token-file", default="~/.config/clawdline/remote-token")
    args = ap.parse_args()

    upstream = args.upstream
    if not upstream:
        port = 7717
        try:
            with open(os.path.expanduser("~/.config/clawdline/config.json")) as f:
                port = json.load(f).get("remote_port", 7717)
        except Exception:
            pass
        upstream = "127.0.0.1:%d" % port

    token = ""
    path = os.path.expanduser(args.token_file)
    if os.path.exists(path):
        with open(path) as f:
            token = f.read().strip()
    else:
        print("no token at %s — /v1/ will answer 401" % path, file=sys.stderr)

    Handler.upstream = upstream
    Handler.token = token
    root = os.path.abspath(args.root)

    class Rooted(Handler):
        def __init__(self, *a, **kw):
            super().__init__(*a, directory=root, **kw)

    # Threaded, because the page is no longer one request. A single-threaded server hands out
    # thirteen files in thirteen round trips, one after another, and the first paint waits for all
    # of them. Still HTTP/1.0 and still a connection per file — `protocol_version` is a property of
    # the handler and this does not change it — but the thirteen are now answered at once.
    http.server.ThreadingHTTPServer.allow_reuse_address = True
    with http.server.ThreadingHTTPServer(("127.0.0.1", args.port), Rooted) as httpd:
        print("serving %s on http://127.0.0.1:%d/ → %s" % (root, args.port, upstream), file=sys.stderr)
        httpd.serve_forever()


if __name__ == "__main__":
    main()
