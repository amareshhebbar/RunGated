import tempfile, json, re, subprocess, threading, time, os, sys, http.server, socketserver, urllib.request

TOK = re.compile(r"\[\[(?:PII|PCI|PHI|SECRET|DATA|FINANCIAL)_[A-Z0-9_]{1,40}_[0-9A-F]{8}\]\]")
SEEN = []

def sse(w, event, obj):
    data = obj if isinstance(obj, str) else json.dumps(obj)
    if event:
        w.write(f"event: {event}\n".encode())
    w.write(f"data: {data}\n\n".encode()); w.flush()

class H(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    def log_message(self, *a): pass
    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
        body = json.loads(raw)
        SEEN.append((self.path, raw.decode(), dict(self.headers)))
        tok = (TOK.findall(raw.decode()) or [""])[0]
        stream = body.get("stream")
        if self.path.endswith("/chat/completions"):
            if not stream:
                out = {"id": "c1", "object": "chat.completion", "created": 1, "model": "mock-1",
                       "choices": [{"index": 0, "finish_reason": "tool_calls", "message": {"role": "assistant", "content": "Emailing " + tok,
                         "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_invoice", "arguments": "{\"invoice_id\":\"INV-000123\"}"}},
                                        {"id": "call_2", "type": "function", "function": {"name": "wipe_database", "arguments": "{}"}}]}}],
                       "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}
                return self.js(out)
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.send_header("Transfer-Encoding", "chunked"); self.end_headers()
            w = Chunked(self.wfile)
            def ch(delta, fin=None, **kw):
                d = {"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-1", "choices": [{"index": 0, "delta": delta, "finish_reason": fin}]}
                d.update(kw); sse(w, None, d)
            ch({"role": "assistant", "content": ""})
            for p in ["Emailing ", tok[:4], tok[4:12], tok[12:]]: ch({"content": p})
            ch({"tool_calls": [{"index": 0, "id": "call_1", "type": "function", "function": {"name": "get_invoice", "arguments": ""}}]})
            ch({"tool_calls": [{"index": 0, "function": {"arguments": "{\"invoice_id\":\"INV-000123\"}"}}]})
            ch({"tool_calls": [{"index": 1, "id": "call_2", "type": "function", "function": {"name": "wipe_database", "arguments": "{}"}}]})
            ch({}, "tool_calls")
            sse(w, None, {"id": "c1", "object": "chat.completion.chunk", "created": 1, "model": "mock-1", "choices": [], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}})
            sse(w, None, "[DONE]"); w.end(); return
        if self.path.endswith("/responses"):
            msg = {"type": "message", "id": "msg_1", "status": "completed", "role": "assistant", "content": [{"type": "output_text", "text": "Emailing " + tok, "annotations": []}]}
            fc1 = {"type": "function_call", "id": "fc_1", "call_id": "call_1", "name": "get_invoice", "arguments": "{\"invoice_id\":\"INV-000123\"}", "status": "completed"}
            fc2 = {"type": "function_call", "id": "fc_2", "call_id": "call_2", "name": "wipe_database", "arguments": "{}", "status": "completed"}
            base = {"id": "resp_1", "object": "response", "created_at": 1, "status": "completed", "model": "mock-1", "parallel_tool_calls": True, "tool_choice": "auto", "tools": [], "output": [msg, fc1, fc2],
                    "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15, "input_tokens_details": {"cached_tokens": 0}, "output_tokens_details": {"reasoning_tokens": 0}}}
            if not stream: return self.js(base)
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.send_header("Transfer-Encoding", "chunked"); self.end_headers()
            w = Chunked(self.wfile); n = [0]
            def ev(t, **kw):
                d = {"type": t, "sequence_number": n[0]}; d.update(kw); n[0] += 1; sse(w, t, d)
            inprog = dict(base, status="in_progress", output=[], usage=None)
            ev("response.created", response=inprog); ev("response.in_progress", response=inprog)
            ev("response.output_item.added", output_index=0, item={"type": "message", "id": "msg_1", "status": "in_progress", "role": "assistant", "content": []})
            ev("response.content_part.added", item_id="msg_1", output_index=0, content_index=0, part={"type": "output_text", "text": "", "annotations": []})
            for p in ["Emailing ", tok[:5], tok[5:13], tok[13:]]:
                ev("response.output_text.delta", item_id="msg_1", output_index=0, content_index=0, delta=p, logprobs=[])
            ev("response.output_text.done", item_id="msg_1", output_index=0, content_index=0, text="Emailing " + tok, logprobs=[])
            ev("response.content_part.done", item_id="msg_1", output_index=0, content_index=0, part={"type": "output_text", "text": "Emailing " + tok, "annotations": []})
            ev("response.output_item.done", output_index=0, item=msg)
            for i, fc in ((1, fc1), (2, fc2)):
                ev("response.output_item.added", output_index=i, item=dict(fc, arguments="", status="in_progress"))
                ev("response.function_call_arguments.delta", item_id=fc["id"], output_index=i, delta=fc["arguments"])
                ev("response.function_call_arguments.done", item_id=fc["id"], output_index=i, arguments=fc["arguments"])
                ev("response.output_item.done", output_index=i, item=fc)
            ev("response.completed", response=base)
            w.end(); return
        if self.path.endswith("/messages"):
            content = [{"type": "text", "text": "Emailing " + tok}, {"type": "tool_use", "id": "tu_1", "name": "get_invoice", "input": {"invoice_id": "INV-000123"}}, {"type": "tool_use", "id": "tu_2", "name": "wipe_database", "input": {}}]
            full = {"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-4", "content": content, "stop_reason": "tool_use", "stop_sequence": None, "usage": {"input_tokens": 10, "output_tokens": 5}}
            if not stream: return self.js(full)
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.send_header("Transfer-Encoding", "chunked"); self.end_headers()
            w = Chunked(self.wfile)
            sse(w, "message_start", {"type": "message_start", "message": dict(full, content=[], stop_reason=None, usage={"input_tokens": 10, "output_tokens": 1})})
            sse(w, "content_block_start", {"type": "content_block_start", "index": 0, "content_block": {"type": "text", "text": ""}})
            for p in ["Emailing ", tok[:3], tok[3:11], tok[11:]]:
                sse(w, "content_block_delta", {"type": "content_block_delta", "index": 0, "delta": {"type": "text_delta", "text": p}})
            sse(w, "content_block_stop", {"type": "content_block_stop", "index": 0})
            for i, (nm, js) in ((1, ("get_invoice", ["{\"invoice_id\":", "\"INV-000123\"}"])), (2, ("wipe_database", ["{}"]))):
                sse(w, "content_block_start", {"type": "content_block_start", "index": i, "content_block": {"type": "tool_use", "id": f"tu_{i}", "name": nm, "input": {}}})
                for j in js: sse(w, "content_block_delta", {"type": "content_block_delta", "index": i, "delta": {"type": "input_json_delta", "partial_json": j}})
                sse(w, "content_block_stop", {"type": "content_block_stop", "index": i})
            sse(w, "message_delta", {"type": "message_delta", "delta": {"stop_reason": "tool_use", "stop_sequence": None}, "usage": {"output_tokens": 20}})
            sse(w, "message_stop", {"type": "message_stop"})
            w.end(); return
        self.send_response(404); self.end_headers()
    def js(self, o):
        b = json.dumps(o).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)

class Chunked:
    def __init__(self, f): self.f = f
    def write(self, b): self.f.write(f"{len(b):x}\r\n".encode() + b + b"\r\n")
    def flush(self): self.f.flush()
    def end(self): self.f.write(b"0\r\n\r\n"); self.f.flush()

class S(socketserver.ThreadingMixIn, http.server.HTTPServer): daemon_threads = True

srv = S(("127.0.0.1", 19999), H); threading.Thread(target=srv.serve_forever, daemon=True).start()
root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
env = dict(os.environ, RUNGATED_OPENAI_BASE_URL="http://127.0.0.1:19999/v1", RUNGATED_ANTHROPIC_BASE_URL="http://127.0.0.1:19999/v1", OPENAI_API_KEY="sk-up", ANTHROPIC_API_KEY="ak-up")
gw = subprocess.Popen([root + "/bin/rungated", "serve", "--dev", "--listen", "127.0.0.1:18080", "--data", tempfile.mkdtemp(prefix="rg-sdk-")], cwd=root, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
try:
    for _ in range(100):
        try: urllib.request.urlopen("http://127.0.0.1:18080/v1/health"); break
        except Exception: time.sleep(0.1)
    from openai import OpenAI
    import anthropic
    hdr = {"X-Rungated-Agent": "finance-agent", "X-Rungated-Session": "sdk-1"}
    oc = OpenAI(base_url="http://127.0.0.1:18080/llm/openai/v1", api_key="unused", default_headers=hdr)
    ac = anthropic.Anthropic(base_url="http://127.0.0.1:18080/llm/anthropic", api_key="unused", default_headers=hdr)
    PII = "Please email carol.customer@clientmail.example about INV-000123"
    ok = True
    def check(name, cond, extra=""):
        global ok; ok = ok and cond; print(("PASS " if cond else "FAIL ") + name, extra if not cond else "")
    r = oc.chat.completions.create(model="mock-1", messages=[{"role": "user", "content": PII}])
    m = r.choices[0].message
    check("openai chat non stream restore", "carol.customer@clientmail.example" in m.content, m.content)
    check("openai chat blocked tool removed", [t.function.name for t in m.tool_calls] == ["get_invoice"], str(m.tool_calls))
    check("openai chat PII not upstream", "carol.customer" not in SEEN[-1][1])
    txt = ""; names = []; fin = None
    for c in oc.chat.completions.create(model="mock-1", messages=[{"role": "user", "content": PII}], stream=True):
        if c.choices:
            d = c.choices[0].delta
            txt += d.content or ""
            for t in d.tool_calls or []:
                if t.function and t.function.name: names.append(t.function.name)
            fin = c.choices[0].finish_reason or fin
    check("openai chat stream restore", "carol.customer@clientmail.example" in txt, txt)
    check("openai chat stream tools", names == ["get_invoice"] and fin == "tool_calls", f"{names} {fin}")
    r = oc.responses.create(model="mock-1", input=PII)
    types = [o.type for o in r.output]
    check("openai responses non stream", types == ["message", "function_call", "message"] and "carol.customer@clientmail.example" in r.output[0].content[0].text, str(types))
    got = []; text = ""
    with oc.responses.stream(model="mock-1", input=PII) as s:
        for e in s:
            got.append(e.type)
            if e.type == "response.output_text.delta": text += e.delta
        final = s.get_final_response()
    check("openai responses stream", "carol.customer@clientmail.example" in text and [o.type for o in final.output] == ["message", "function_call", "message"], f"{text} {[o.type for o in final.output]}")
    r = ac.messages.create(model="claude-sonnet-4", max_tokens=100, messages=[{"role": "user", "content": PII}])
    kinds = [b.type for b in r.content]
    check("anthropic non stream", kinds == ["text", "tool_use", "text"] and r.stop_reason == "tool_use" and "carol.customer@clientmail.example" in r.content[0].text, f"{kinds} {r.stop_reason}")
    with ac.messages.stream(model="claude-sonnet-4", max_tokens=100, messages=[{"role": "user", "content": PII}]) as s:
        text = "".join(s.text_stream)
        fm = s.get_final_message()
    check("anthropic stream", "carol.customer@clientmail.example" in text and [b.type for b in fm.content] == ["text", "tool_use", "text"], f"{text} {[b.type for b in fm.content]}")
    try:
        oc.chat.completions.create(model="forbidden-model", messages=[{"role": "user", "content": "hi"}]); check("blocked model raises", False)
    except Exception as e:
        check("blocked model raises", "403" in str(e) or "rungated" in str(e).lower(), str(e)[:200])
    print("ALL PASS" if ok else "SOME FAILED")
    sys.exit(0 if ok else 1)
finally:
    gw.terminate()
