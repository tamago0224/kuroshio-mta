#!/usr/bin/env python3
"""Local-only demonstration of kuroshio.smtp-policy.v1; no request logging."""
import argparse
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VERSION = "kuroshio.smtp-policy.v1"


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        if self.path != "/policy":
            self.send_error(404)
            return
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 16 * 1024 * 1024:
                raise ValueError("invalid request size")
            request = json.loads(self.rfile.read(size))
            if request.get("version") != VERSION or request.get("stage") not in ("rcpt", "data"):
                raise ValueError("unsupported schema")
            action = "accept"
            if request["stage"] == "rcpt":
                recipient = request.get("recipient", "")
                if recipient.startswith("reject@"):
                    action = "reject"
                elif recipient.startswith("tempfail@"):
                    action = "tempfail"
            elif any(address.startswith("data-reject@") for address in request.get("recipients", [])):
                action = "reject"
            body = json.dumps({"version": VERSION, "action": action}).encode()
        except (ValueError, TypeError, KeyError):
            self.send_error(400)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=9081)
    args = parser.parse_args()
    server = ThreadingHTTPServer(("127.0.0.1", args.port), Handler)
    server.daemon_threads = True
    print(f"Demo policy hook on 127.0.0.1:{server.server_port}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
