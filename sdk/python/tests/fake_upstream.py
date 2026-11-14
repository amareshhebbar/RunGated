import http.server
import json
import re
import socketserver
import threading

TOK = re.compile(r"\[\[(?:PII|PCI|PHI|SECRET|DATA|FINANCIAL)_[A-Z0-9_]{1,40}_[0-9A-F]{8}\]\]")


class Chunked:
    def __init__(self, f):
        self.f = f

    def write(self, b):
        self.f.write(f"{len(b):x}\r\n".encode() + b + b"\r\n")

    def flush(self):
        self.f.flush()

    def end(self):
        self.f.write(b"0\r\n\r\n")
        self.f.flush()


def sse(w, event, obj):
    data = obj if isinstance(obj, str) else json.dumps(obj)
    if event:
        w.write(f"event: {event}\n".encode())
    w.write(f"data: {data}\n\n".encode())
    w.flush()


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    seen = []

    def log_message(self, *a):
        pass

    def js(self, o):
        b = json.dumps(o).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        body = json.loads(raw)
        type(self).seen.append((self.path, raw.decode()))
        tok = (TOK.findall(raw.decode()) or [""])[0]
        if self.path.endswith("/chat/completions") and not body.get("stream"):
            return self.js(
                {
                    "id": "c1",
                    "object": "chat.completion",
                    "created": 1,
                    "model": "mock-1",
                    "choices": [
                        {
                            "index": 0,
                            "finish_reason": "tool_calls",
                            "message": {
                                "role": "assistant",
                                "content": "Emailing " + tok,
                                "tool_calls": [
                                    {"id": "call_1", "type": "function", "function": {"name": "get_invoice", "arguments": "{\"invoice_id\":\"INV-000123\"}"}},
                                    {"id": "call_2", "type": "function", "function": {"name": "wipe_database", "arguments": "{}"}},
                                ],
                            },
                        }
                    ],
                    "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
                }
            )
        if self.path.endswith("/messages") and not body.get("stream"):
            content = [
                {"type": "text", "text": "Emailing " + tok},
                {"type": "tool_use", "id": "tu_1", "name": "get_invoice", "input": {"invoice_id": "INV-000123"}},
                {"type": "tool_use", "id": "tu_2", "name": "wipe_database", "input": {}},
            ]
            return self.js(
                {
                    "id": "msg_1",
                    "type": "message",
                    "role": "assistant",
                    "model": "claude-sonnet-4",
                    "content": content,
                    "stop_reason": "tool_use",
                    "stop_sequence": None,
                    "usage": {"input_tokens": 10, "output_tokens": 5},
                }
            )
        self.send_response(404)
        self.end_headers()


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def start(port):
    Handler.seen = []
    srv = Server(("127.0.0.1", port), Handler)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv, Handler
