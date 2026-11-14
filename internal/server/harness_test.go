package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amareshhebbar/rungated/internal/config"
)

const testManifest = `apiVersion: rungated.dev/v0
kind: Agent
metadata:
  id: t-agent
  owner: qa@acme.example
  purpose: Answers invoice questions for the finance team
  tenant: acme
spec:
  model:
    providers: [openai, anthropic]
    allowed: ["mock-*", "gpt-4o*", "claude-*"]
  riskTier: high
  autonomy:
    default: auto_contain
    overrides:
      - tool: transfer
        mode: human
  tools:
    - name: get_invoice
      server: fin
      blastRadius: low
      args:
        invoice_id: {type: string, required: true, pattern: "^INV-[0-9]{6}$"}
    - name: send_email
      server: fin
      blastRadius: high
      args:
        to: {type: string, required: true, like: ["*@acme.example"]}
        body: {type: string, maxLength: 5000}
    - name: transfer
      server: fin
      blastRadius: high
      requireApproval: true
      args:
        amount: {type: number, required: true, max: 5000}
  mcpServers:
    - name: fin
      url: http://localhost:1/mcp
  egress:
    allow: ["api.acme.example"]
  limits:
    requestsPerMinute: 6000
    toolCallsPerMinute: 6000
  escalation:
    timeoutSeconds: 20
    onTimeout: deny
`

type capture struct {
	Path   string
	Raw    string
	Body   map[string]any
	Header http.Header
}

type fakeUp struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []capture
	handler func(path string, body map[string]any, w http.ResponseWriter, r *http.Request)
}

func newFakeUp(t *testing.T) *fakeUp {
	f := &fakeUp{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.reqs = append(f.reqs, capture{Path: r.URL.Path, Raw: string(raw), Body: body, Header: r.Header.Clone()})
		h := f.handler
		f.mu.Unlock()
		if h == nil {
			http.Error(w, "no handler", 500)
			return
		}
		h(r.URL.Path, body, w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeUp) last() capture {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reqs[len(f.reqs)-1]
}

func (f *fakeUp) set(h func(path string, body map[string]any, w http.ResponseWriter, r *http.Request)) {
	f.mu.Lock()
	f.handler = h
	f.mu.Unlock()
}

type env struct {
	t   *testing.T
	app *App
	srv *httptest.Server
	up  *fakeUp
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	up := newFakeUp(t)
	cfg := config.Default()
	cfg.Dev = true
	cfg.DataDir = t.TempDir()
	cfg.Upstreams.OpenAI = config.Upstream{BaseURL: up.srv.URL + "/v1", APIKey: "sk-upstream"}
	cfg.Upstreams.Anthropic = config.Upstream{BaseURL: up.srv.URL + "/v1", APIKey: "ak-upstream"}
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Finalize(); err != nil {
		t.Fatal(err)
	}
	app, err := New(context.Background(), cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	if _, err := app.Registry.Register(context.Background(), []byte(testManifest), false); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app.Mux)
	t.Cleanup(srv.Close)
	return &env{t: t, app: app, srv: srv, up: up}
}

func (e *env) do(method, path string, body any, headers map[string]string) (int, http.Header, []byte) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		case []byte:
			rd = bytes.NewReader(b)
		default:
			raw, _ := json.Marshal(b)
			rd = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rd)
	if err != nil {
		e.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Rungated-Agent", "t-agent")
	req.Header.Set("X-Rungated-Session", "sess-1")
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func jsonOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not json: %s", b)
	}
	return m
}

type sseEv struct {
	Event string
	Data  string
}

func parseSSE(b []byte) []sseEv {
	var out []sseEv
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var cur sseEv
	has := false
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if has {
				out = append(out, cur)
			}
			cur, has = sseEv{}, false
		case strings.HasPrefix(line, ":"):
		case strings.HasPrefix(line, "event: "):
			cur.Event = strings.TrimPrefix(line, "event: ")
			has = true
		case strings.HasPrefix(line, "data: "):
			if cur.Data != "" {
				cur.Data += "\n"
			}
			cur.Data += strings.TrimPrefix(line, "data: ")
			has = true
		}
	}
	if has {
		out = append(out, cur)
	}
	return out
}

func writeSSE(w http.ResponseWriter, event string, data any) {
	var s string
	switch d := data.(type) {
	case string:
		s = d
	default:
		b, _ := json.Marshal(d)
		s = string(b)
	}
	if event != "" {
		io.WriteString(w, "event: "+event+"\n")
	}
	io.WriteString(w, "data: "+s+"\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseHeader(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
}

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
