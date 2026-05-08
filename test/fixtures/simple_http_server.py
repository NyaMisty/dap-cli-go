from __future__ import annotations

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

ROOT = Path(__file__).resolve().parent
START_FILE = ROOT / "server_started.txt"
REQUEST_FILE = ROOT / "server_request.txt"
DONE_FILE = ROOT / "server_done.txt"
PORT = 0


class Handler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:
        port = self.server.server_address[1]
        if self.path == "/shutdown":
            payload = json.dumps({"path": self.path, "message": "bye", "port": port}).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            self.server.shutdown()
            return

        data = {"path": self.path, "message": "hello", "port": port}
        REQUEST_FILE.write_text(json.dumps(data), encoding="utf-8")
        payload = json.dumps(data).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)
        post_response_marker = self.path
        _ = post_response_marker

    def log_message(self, format: str, *args) -> None:
        return


def main() -> None:
    for path in (START_FILE, REQUEST_FILE, DONE_FILE):
        try:
            path.unlink()
        except FileNotFoundError:
            pass

    server = ThreadingHTTPServer(("127.0.0.1", PORT), Handler)
    START_FILE.write_text(str(server.server_address[1]), encoding="utf-8")
    try:
        server.serve_forever(poll_interval=0.1)
    finally:
        server.server_close()
        DONE_FILE.write_text("done", encoding="utf-8")


if __name__ == "__main__":
    main()
