package mcpproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/jsonx"
	"github.com/amareshhebbar/rungated/internal/sse"
)

type Proxy struct {
	Engine      *gateway.Engine
	Resolver    *auth.Resolver
	Interceptor *Interceptor
	Client      *http.Client
	MaxBody     int64
	KeepAlive   time.Duration
	mu          sync.Mutex
	corr        map[string]map[string]corrEntry
}

type corrEntry struct {
	c  Corr
	at time.Time
}

func NewProxy(e *gateway.Engine, r *auth.Resolver, i *Interceptor, maxBody int64) *Proxy {
	if maxBody <= 0 {
		maxBody = 16 << 20
	}
	return &Proxy{
		Engine: e, Resolver: r, Interceptor: i, MaxBody: maxBody, KeepAlive: 5 * time.Second,
		Client: &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, ResponseHeaderTimeout: 5 * time.Minute, MaxIdleConnsPerHost: 64, IdleConnTimeout: 90 * time.Second}},
		corr:   map[string]map[string]corrEntry{},
	}
}

var forwardHeaders = []string{"Content-Type", "Mcp-Session-Id", "Mcp-Protocol-Version", "Last-Event-ID", "User-Agent"}
var returnHeaders = []string{"Mcp-Session-Id", "Mcp-Protocol-Version"}

func (p *Proxy) setCorr(key, id string, c Corr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.corr[key]
	if m == nil {
		m = map[string]corrEntry{}
		p.corr[key] = m
	}
	now := time.Now()
	if len(m) > 2000 {
		for k, e := range m {
			if now.Sub(e.at) > 10*time.Minute {
				delete(m, k)
			}
		}
	}
	m[id] = corrEntry{c: c, at: now}
}

func (p *Proxy) takeCorr(key, id string) (Corr, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	m := p.corr[key]
	e, ok := m[id]
	if ok {
		delete(m, id)
		if len(m) == 0 {
			delete(p.corr, key)
		}
	}
	return e.c, ok
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(jsonx.Marshal(v))
}

func serverName(path string) string {
	n := strings.TrimPrefix(path, "/mcp/")
	n = strings.Trim(n, "/")
	if strings.Contains(n, "/") {
		return ""
	}
	return n
}

type responder struct {
	w       http.ResponseWriter
	sw      *sse.Writer
	accepts bool
	sse     bool
	batch   bool
	ka      time.Duration
	buf     []Message
}

func (r *responder) startSSE() {
	if r.sse {
		return
	}
	h := r.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	r.w.WriteHeader(http.StatusOK)
	r.sse = true
	r.sw = sse.NewWriter(r.w)
	r.sw.Comment("rungated")
}

func (r *responder) hook() func() {
	if !r.accepts {
		return nil
	}
	r.startSSE()
	done := make(chan struct{})
	fin := make(chan struct{})
	go func() {
		defer close(fin)
		t := time.NewTicker(r.ka)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				r.sw.Comment("rungated waiting for approval")
			}
		}
	}()
	return func() {
		close(done)
		<-fin
	}
}

func (r *responder) emit(m Message) {
	if r.sse {
		r.sw.Write(&sse.Event{Event: "message", Data: string(Encode(m))})
		return
	}
	r.buf = append(r.buf, m)
}

func (r *responder) finish() {
	if r.sse {
		return
	}
	if len(r.buf) == 0 {
		r.w.WriteHeader(http.StatusAccepted)
		return
	}
	r.w.Header().Set("Content-Type", "application/json")
	r.w.WriteHeader(http.StatusOK)
	r.w.Write(EncodeAll(r.buf, r.batch))
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := serverName(r.URL.Path)
	if name == "" {
		http.NotFound(w, r)
		return
	}
	id, err := p.Resolver.Resolve(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return
	}
	ag := p.Engine.Registry.Get(id.AgentID)
	if ag == nil {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "unknown agent " + id.AgentID})
		return
	}
	srv := ag.Manifest.FindServer(name)
	if srv == nil || srv.URL == "" {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "mcp server " + name + " is not declared with a url in the manifest"})
		return
	}
	sc := Scope{Agent: id.AgentID, Session: id.SessionID, Server: name}
	if sc.Session == "" {
		sc.Session = r.Header.Get("Mcp-Session-Id")
	}
	switch r.Method {
	case http.MethodPost:
		p.post(w, r, sc, srv.URL, srv.Headers)
	case http.MethodGet:
		p.get(w, r, sc, srv.URL, srv.Headers)
	case http.MethodDelete:
		p.del(w, r, srv.URL, srv.Headers)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Proxy) upstreamReq(ctx context.Context, method, url string, body io.Reader, src *http.Request, extra map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	for _, h := range forwardHeaders {
		if v := src.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	req.Header.Set("Accept-Encoding", "identity")
	if method == http.MethodPost {
		req.Header.Set("Accept", "application/json, text/event-stream")
		if req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/json")
		}
	} else if method == http.MethodGet {
		req.Header.Set("Accept", "text/event-stream")
	}
	for k, v := range extra {
		req.Header.Set(k, os.ExpandEnv(v))
	}
	return req, nil
}

func (p *Proxy) post(w http.ResponseWriter, r *http.Request, sc Scope, url string, hdrs map[string]string) {
	body, err := io.ReadAll(io.LimitReader(r.Body, p.MaxBody+1))
	if err != nil || int64(len(body)) > p.MaxBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "request body too large or unreadable"})
		return
	}
	msgs, batch, err := Parse(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorReply(Message{"id": nil}, -32700, "parse error"))
		return
	}
	acc := r.Header.Get("Accept")
	rs := &responder{w: w, accepts: strings.Contains(acc, "text/event-stream"), batch: batch, ka: p.KeepAlive}
	sessKey := sc.Agent + "|" + sc.Server + "|" + r.Header.Get("Mcp-Session-Id")
	var fwd []Message
	pending := map[string]bool{}
	for _, m := range msgs {
		ok, reply, corr := p.Interceptor.Client(r.Context(), sc, m, rs.hook)
		if !ok {
			if reply != nil {
				rs.emit(reply)
			}
			continue
		}
		fwd = append(fwd, m)
		if IsRequest(m) {
			k := IDKey(m)
			p.setCorr(sessKey, k, corr)
			pending[k] = true
		}
	}
	defer func() {
		for k := range pending {
			p.takeCorr(sessKey, k)
		}
	}()
	if len(fwd) == 0 {
		rs.finish()
		return
	}
	req, err := p.upstreamReq(r.Context(), http.MethodPost, url, bytes.NewReader(EncodeAll(fwd, batch)), r, hdrs)
	if err != nil {
		p.upstreamFailed(rs, fwd, err)
		return
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		p.upstreamFailed(rs, fwd, err)
		return
	}
	defer resp.Body.Close()
	if !rs.sse {
		for _, h := range returnHeaders {
			if v := resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
	}
	if resp.StatusCode >= 300 {
		if rs.sse {
			p.upstreamFailed(rs, fwd, errors.New("upstream returned "+resp.Status))
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(raw)
		return
	}
	if resp.StatusCode == http.StatusAccepted && resp.ContentLength <= 0 {
		rs.finish()
		return
	}
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "text/event-stream") {
		rs.startSSE()
		p.relaySSE(r.Context(), rs, resp.Body, sc, sessKey, true)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		p.upstreamFailed(rs, fwd, err)
		return
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		rs.finish()
		return
	}
	out, _, err := Parse(raw)
	if err != nil {
		p.upstreamFailed(rs, fwd, errors.New("upstream sent invalid JSON-RPC"))
		return
	}
	for _, m := range out {
		rs.emit(p.filter(r.Context(), rs, sc, sessKey, m))
	}
	rs.finish()
}

func (p *Proxy) filter(ctx context.Context, rs *responder, sc Scope, sessKey string, m Message) Message {
	if !IsResponse(m) {
		return m
	}
	corr, ok := p.takeCorr(sessKey, IDKey(m))
	if !ok {
		return m
	}
	return p.Interceptor.Server(ctx, sc, corr, m, rs.hook)
}

func (p *Proxy) relaySSE(ctx context.Context, rs *responder, body io.Reader, sc Scope, sessKey string, track bool) {
	rd := sse.NewReader(body)
	for {
		ev, err := rd.Next()
		if err != nil {
			return
		}
		if ev.Data == "" {
			if ev.Comment != "" {
				rs.sw.Write(&sse.Event{Comment: ev.Comment})
			}
			continue
		}
		msgs, batch, perr := Parse([]byte(ev.Data))
		if perr != nil {
			rs.sw.Write(ev)
			continue
		}
		for k, m := range msgs {
			if track {
				msgs[k] = p.filter(ctx, rs, sc, sessKey, m)
			}
		}
		rs.sw.Write(&sse.Event{Event: ev.Event, ID: ev.ID, Retry: ev.Retry, Data: string(EncodeAll(msgs, batch))})
	}
}

func (p *Proxy) upstreamFailed(rs *responder, fwd []Message, err error) {
	slog.Warn("mcp upstream failed", "err", err)
	for _, m := range fwd {
		if IsRequest(m) {
			rs.emit(ErrorReply(m, -32000, "upstream mcp server unavailable: "+err.Error()))
		}
	}
	if !rs.sse && len(rs.buf) == 0 {
		writeJSON(rs.w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	rs.finish()
}

func (p *Proxy) get(w http.ResponseWriter, r *http.Request, sc Scope, url string, hdrs map[string]string) {
	req, err := p.upstreamReq(r.Context(), http.MethodGet, url, nil, r, hdrs)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		w.Write(raw)
		return
	}
	rs := &responder{w: w, accepts: true, ka: p.KeepAlive}
	rs.startSSE()
	p.relaySSE(r.Context(), rs, resp.Body, sc, "", false)
}

func (p *Proxy) del(w http.ResponseWriter, r *http.Request, url string, hdrs map[string]string) {
	req, err := p.upstreamReq(r.Context(), http.MethodDelete, url, nil, r, hdrs)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	w.WriteHeader(resp.StatusCode)
}
