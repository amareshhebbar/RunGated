package llmproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/metrics"
	"github.com/amareshhebbar/rungated/internal/sse"
	"github.com/amareshhebbar/rungated/internal/vault"
)

type Upstream struct {
	BaseURL string
	APIKey  string
}

type Price struct {
	In  float64
	Out float64
}

type Config struct {
	OpenAI    Upstream
	Anthropic Upstream
	Extra     map[string]Upstream
	Prices    map[string]Price
	MaxBody   int64
}

type Proxy struct {
	Engine   *gateway.Engine
	Resolver *auth.Resolver
	Cfg      Config
	Client   *http.Client
	Metrics  *metrics.Metrics
}

func NewClient() *http.Client {
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 180 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
	}
	return &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func New(e *gateway.Engine, r *auth.Resolver, cfg Config, m *metrics.Metrics) *Proxy {
	if cfg.MaxBody <= 0 {
		cfg.MaxBody = 20 << 20
	}
	return &Proxy{Engine: e, Resolver: r, Cfg: cfg, Client: NewClient(), Metrics: m}
}

func route(path string) (provider, rest string, ok bool) {
	for _, p := range []string{"openai", "anthropic"} {
		pre := "/llm/" + p
		if path == pre || strings.HasPrefix(path, pre+"/") {
			rest = strings.TrimPrefix(path, pre)
			rest = strings.TrimPrefix(rest, "/v1")
			if rest == "" {
				rest = "/"
			}
			return p, rest, true
		}
	}
	return "", "", false
}

func classify(provider, method, rest string) API {
	if method != http.MethodPost {
		return ""
	}
	switch provider {
	case "openai":
		switch rest {
		case "/chat/completions":
			return APIChat
		case "/responses":
			return APIResponses
		}
	case "anthropic":
		if rest == "/messages" {
			return APIMessages
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(marshal(v))
}

func writeErr(w http.ResponseWriter, provider string, status int, typ, msg string) {
	if provider == "anthropic" {
		writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": msg}})
		return
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": typ, "param": nil, "code": typ}})
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	provider, rest, ok := route(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, err := p.Resolver.Resolve(r)
	if err != nil {
		writeErr(w, provider, http.StatusUnauthorized, "authentication_error", err.Error())
		return
	}
	api := classify(provider, r.Method, rest)
	if api == "" {
		p.passthrough(w, r, provider, rest)
		return
	}
	p.guarded(w, r, provider, rest, api, id)
}

func (p *Proxy) upstream(provider string, r *http.Request) (Upstream, error) {
	if provider == "anthropic" {
		return p.Cfg.Anthropic, nil
	}
	if name := r.Header.Get("X-Rungated-Upstream"); name != "" {
		u, ok := p.Cfg.Extra[name]
		if !ok {
			return Upstream{}, fmt.Errorf("unknown upstream %q", name)
		}
		return u, nil
	}
	return p.Cfg.OpenAI, nil
}

var hop = map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true}

func bearerOf(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (p *Proxy) upstreamHeaders(dst, src http.Header, provider string, up Upstream) error {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hop[ck] || ck == "Host" || ck == "Content-Length" || ck == "Accept-Encoding" || ck == "Cookie" || strings.HasPrefix(ck, "X-Rungated-") {
			continue
		}
		if ck == "Authorization" && auth.IsAgentToken(bearerOf(vs[0])) {
			continue
		}
		if ck == "X-Api-Key" && auth.IsAgentToken(vs[0]) {
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
	dst.Set("Accept-Encoding", "identity")
	if up.APIKey != "" {
		if provider == "anthropic" {
			dst.Set("X-Api-Key", up.APIKey)
			dst.Del("Authorization")
		} else {
			dst.Set("Authorization", "Bearer "+up.APIKey)
		}
	} else if provider == "anthropic" {
		if dst.Get("X-Api-Key") == "" && dst.Get("Authorization") == "" {
			return errors.New("no upstream api key configured for anthropic")
		}
	} else if dst.Get("Authorization") == "" {
		return errors.New("no upstream api key configured for openai")
	}
	if provider == "anthropic" && dst.Get("Anthropic-Version") == "" {
		dst.Set("Anthropic-Version", "2023-06-01")
	}
	return nil
}

func joinURL(base, rest, rawQuery string) string {
	u := strings.TrimRight(base, "/") + rest
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return u
}

func copyResp(dst, src http.Header, skipLen bool) {
	for k, vs := range src {
		ck := http.CanonicalHeaderKey(k)
		if hop[ck] || ck == "Content-Encoding" || (skipLen && ck == "Content-Length") {
			continue
		}
		for _, v := range vs {
			dst.Add(ck, v)
		}
	}
}

func (p *Proxy) passthrough(w http.ResponseWriter, r *http.Request, provider, rest string) {
	up, err := p.upstream(provider, r)
	if err != nil {
		writeErr(w, provider, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, joinURL(up.BaseURL, rest, r.URL.RawQuery), http.MaxBytesReader(w, r.Body, p.Cfg.MaxBody))
	if err != nil {
		writeErr(w, provider, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if err := p.upstreamHeaders(req.Header, r.Header, provider, up); err != nil {
		writeErr(w, provider, http.StatusBadGateway, "rungated_upstream_not_configured", err.Error())
		return
	}
	req.ContentLength = r.ContentLength
	resp, err := p.Client.Do(req)
	if err != nil {
		writeErr(w, provider, http.StatusBadGateway, "rungated_upstream_error", "upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	copyResp(w.Header(), resp.Header, false)
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 32*1024)
	f, _ := w.(http.Flusher)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if f != nil {
				f.Flush()
			}
		}
		if rerr != nil {
			return
		}
	}
}

func (p *Proxy) cost(model string, in, out int) float64 {
	best := ""
	for k := range p.Cfg.Prices {
		if strings.HasPrefix(model, k) && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return 0
	}
	pr := p.Cfg.Prices[best]
	return (float64(in)*pr.In + float64(out)*pr.Out) / 1e6
}

type guard struct {
	p           *Proxy
	ctx         context.Context
	id          *auth.Identity
	agent       *gateway.Agent
	tenant      string
	provider    string
	model       string
	api         API
	w           *sse.Writer
	restoreArgs bool
}

func (g *guard) base() *core.Action {
	return &core.Action{AgentID: g.id.AgentID, SessionID: g.id.SessionID, TenantID: g.tenant, Channel: core.ChannelLLM, Provider: g.provider, Model: g.model}
}

func (g *guard) restore(s string) string {
	if s == "" || !strings.Contains(s, "[[") {
		return s
	}
	return g.p.Engine.Vault.Restore(g.tenant, g.id.SessionID, s)
}

func (g *guard) await(o *gateway.Outcome) {
	if o.Decision.Verdict != core.VerdictHold {
		return
	}
	if g.w == nil {
		g.p.Engine.Await(g.ctx, o)
		return
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-g.ctx.Done():
				return
			case <-t.C:
				g.w.Comment("rungated awaiting approval")
			}
		}
	}()
	g.p.Engine.Await(g.ctx, o)
	close(done)
}

func (g *guard) decideResponse(texts []core.TextPart, in, out int) *gateway.Outcome {
	a := g.base()
	a.Kind = core.KindLLMResponse
	a.Texts = texts
	a.Tokens = in + out
	a.CostUSD = g.p.cost(g.model, in, out)
	a.Meta = map[string]string{"api": string(g.api), "input_tokens": strconv.Itoa(in), "output_tokens": strconv.Itoa(out)}
	o := g.p.Engine.Decide(g.ctx, a)
	g.await(o)
	g.p.Engine.Usage(a, a.Tokens, a.CostUSD)
	return o
}

func (g *guard) resolve(name string) (server, tool string) {
	if g.agent == nil {
		return "", name
	}
	m := g.agent.Manifest
	for _, t := range m.Spec.Tools {
		if t.Name == name {
			return t.Server, t.Name
		}
	}
	for _, t := range m.Spec.Tools {
		if t.Server == "" {
			continue
		}
		for _, sep := range []string{"__", ".", "/", ":", "-"} {
			if name == t.Server+sep+t.Name || name == "mcp"+sep+t.Server+sep+t.Name {
				return t.Server, t.Name
			}
		}
	}
	return "", name
}

func (g *guard) call(name string, args map[string]any, argsText string) (ok bool, msg string, newArgs map[string]any, changed bool) {
	server, tool := g.resolve(name)
	callArgs := args
	if g.restoreArgs && vault.TokenRe.MatchString(argsText) {
		if r, isMap := g.p.Engine.Vault.RestoreAny(g.tenant, g.id.SessionID, args).(map[string]any); isMap {
			callArgs = r
			changed = true
		}
	}
	a := g.base()
	a.Kind = core.KindToolCall
	a.Server = server
	a.Tool = tool
	a.Args = callArgs
	a.Meta = map[string]string{"proposed": "1", "api": string(g.api)}
	o := g.p.Engine.Decide(g.ctx, a)
	g.await(o)
	if !o.Allowed {
		m := o.Message()
		if m == "" {
			m = "Rungated blocked tool call " + name
		}
		return false, m, nil, false
	}
	return true, "", callArgs, changed
}

func (g *guard) cheap(text string) bool {
	if len(text) > 4096 {
		text = text[len(text)-4096:]
	}
	if len(g.p.Engine.Detect.Secrets.Scan(0, text)) > 0 {
		return true
	}
	if g.agent != nil {
		a := &core.Action{Kind: core.KindLLMResponse, Texts: []core.TextPart{{Source: core.SourceAssistant, Text: text}}}
		return len(detect.ExfilScan(a, g.agent.Manifest)) > 0
	}
	return false
}

func setHeaders(w http.ResponseWriter, o *gateway.Outcome) {
	h := w.Header()
	h.Set("X-Rungated-Verdict", string(o.Decision.Verdict))
	h.Set("X-Rungated-Score", strconv.Itoa(o.Decision.Score))
	h.Set("X-Rungated-Action-Id", o.Action.ID)
	if o.Decision.ApprovalID != "" {
		h.Set("X-Rungated-Approval", o.Decision.ApprovalID+":"+o.Decision.ApprovalStatus)
	}
}

func (p *Proxy) record(api API, status int, added, up time.Duration) {
	if p.Metrics != nil {
		p.Metrics.ObserveProxy(string(api), status, added, up)
	}
}

func (p *Proxy) guarded(w http.ResponseWriter, r *http.Request, provider, rest string, api API, id *auth.Identity) {
	start := time.Now()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, p.Cfg.MaxBody))
	if err != nil {
		writeErr(w, provider, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body too large or unreadable")
		return
	}
	body, err := decode(raw)
	if err != nil {
		writeErr(w, provider, http.StatusBadRequest, "invalid_request_error", "request body is not valid JSON")
		return
	}
	stream, _ := body["stream"].(bool)
	model := str(body, "model")
	a := &core.Action{AgentID: id.AgentID, SessionID: id.SessionID, Channel: core.ChannelLLM, Kind: core.KindLLMRequest, Provider: provider, Model: model}
	a.Texts = ExtractRequest(api, body)
	a.Meta = map[string]string{"max_tokens": strconv.Itoa(MaxTokens(api, body)), "api": string(api), "stream": strconv.FormatBool(stream)}
	ctx := r.Context()
	out := p.Engine.Decide(ctx, a)
	if out.Decision.Verdict == core.VerdictHold {
		p.Engine.Await(ctx, out)
	}
	setHeaders(w, out)
	if !out.Allowed {
		msg := out.Message()
		if msg == "" {
			msg = "Rungated rejected this request: " + out.Decision.Summary()
		}
		if provider == "anthropic" {
			writeErr(w, provider, http.StatusForbidden, "permission_error", msg)
		} else {
			writeErr(w, provider, http.StatusForbidden, "rungated_blocked", msg)
		}
		p.record(api, http.StatusForbidden, time.Since(start), 0)
		return
	}
	outBytes := raw
	modified := false
	if out.Texts != nil {
		for i, t := range out.Texts {
			if i < len(a.Texts) && a.Texts[i].Path != "" && t != a.Texts[i].Text {
				if setPath(body, a.Texts[i].Path, t) {
					modified = true
				}
			}
		}
	}
	injectedUsage := false
	if stream && api == APIChat {
		so := obj(body["stream_options"])
		if so == nil {
			body["stream_options"] = map[string]any{"include_usage": true}
			injectedUsage = true
			modified = true
		} else if v, _ := so["include_usage"].(bool); !v {
			so["include_usage"] = true
			injectedUsage = true
			modified = true
		}
	}
	if modified {
		outBytes = marshal(body)
	}
	up, err := p.upstream(provider, r)
	if err != nil {
		writeErr(w, provider, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(up.BaseURL, rest, r.URL.RawQuery), bytes.NewReader(outBytes))
	if err != nil {
		writeErr(w, provider, http.StatusInternalServerError, "rungated_internal_error", err.Error())
		return
	}
	if err := p.upstreamHeaders(req.Header, r.Header, provider, up); err != nil {
		writeErr(w, provider, http.StatusBadGateway, "rungated_upstream_not_configured", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	pre := time.Since(start)
	upStart := time.Now()
	resp, err := p.Client.Do(req)
	if err != nil {
		slog.Warn("upstream request failed", "provider", provider, "err", err)
		writeErr(w, provider, http.StatusBadGateway, "rungated_upstream_error", "upstream request failed: "+err.Error())
		p.record(api, http.StatusBadGateway, pre, 0)
		return
	}
	defer resp.Body.Close()
	upHeaders := time.Since(upStart)

	g := &guard{p: p, ctx: ctx, id: id, agent: out.Agent, tenant: out.Action.TenantID, provider: provider, model: model, api: api, restoreArgs: true}
	if out.Agent != nil {
		pv := out.Agent.Manifest.Spec.Privacy.RestoreInToolArgs
		g.restoreArgs = pv == nil || *pv
	}

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode/100 != 2 {
		copyResp(w.Header(), resp.Header, false)
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		p.record(api, resp.StatusCode, pre, upHeaders)
		return
	}
	if stream && strings.HasPrefix(ct, "text/event-stream") {
		copyResp(w.Header(), resp.Header, true)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		g.w = sse.NewWriter(w)
		g.runStream(resp.Body, injectedUsage)
		p.record(api, http.StatusOK, pre, time.Since(upStart))
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		writeErr(w, provider, http.StatusBadGateway, "rungated_upstream_error", "reading upstream response failed: "+err.Error())
		return
	}
	upTotal := time.Since(upStart)
	postStart := time.Now()
	rb, derr := decode(data)
	if derr != nil {
		copyResp(w.Header(), resp.Header, true)
		w.WriteHeader(resp.StatusCode)
		w.Write(data)
		p.record(api, resp.StatusCode, pre, upTotal)
		return
	}
	g.process(rb)
	final := marshal(rb)
	copyResp(w.Header(), resp.Header, true)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(final)))
	w.WriteHeader(resp.StatusCode)
	w.Write(final)
	p.record(api, resp.StatusCode, pre+time.Since(postStart), upTotal)
}

func (g *guard) process(body map[string]any) {
	pr := ParseResponse(g.api, body)
	o := g.decideResponse(pr.Texts, pr.InTokens, pr.OutTokens)
	if !o.Allowed {
		g.neutralize(body, o.Message())
		return
	}
	for i, t := range pr.Texts {
		txt := t.Text
		if o.Texts != nil && i < len(o.Texts) {
			txt = o.Texts[i]
		}
		if r := g.restore(txt); r != t.Text {
			setPath(body, t.Path, r)
		}
	}
	switch g.api {
	case APIChat:
		g.guardChat(body)
	case APIResponses:
		g.guardResponses(body)
	case APIMessages:
		g.guardMessages(body)
	}
}

func messageItem(text string) map[string]any {
	return map[string]any{
		"type": "message", "id": core.NewID("msg"), "status": "completed", "role": "assistant",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}},
	}
}

func (g *guard) neutralize(body map[string]any, msg string) {
	switch g.api {
	case APIChat:
		for _, cv := range arr(body["choices"]) {
			c := obj(cv)
			m := obj(c["message"])
			if m == nil {
				continue
			}
			m["content"] = msg
			delete(m, "tool_calls")
			delete(m, "function_call")
			c["finish_reason"] = "stop"
		}
	case APIResponses:
		body["output"] = []any{messageItem(msg)}
	case APIMessages:
		body["content"] = []any{map[string]any{"type": "text", "text": msg}}
		body["stop_reason"] = "end_turn"
	}
}

func joinNotes(existing string, notes []string) string {
	add := strings.Join(notes, "\n")
	if strings.TrimSpace(existing) == "" {
		return add
	}
	return existing + "\n\n" + add
}

func (g *guard) guardChat(body map[string]any) {
	for _, cv := range arr(body["choices"]) {
		c := obj(cv)
		m := obj(c["message"])
		calls := arr(m["tool_calls"])
		if len(calls) == 0 {
			continue
		}
		var kept []any
		var notes []string
		for _, tv := range calls {
			t := obj(tv)
			fn := obj(t["function"])
			argsText := str(fn, "arguments")
			ok, msg, newArgs, changed := g.call(str(fn, "name"), parseArgs(argsText), argsText)
			if !ok {
				notes = append(notes, msg)
				continue
			}
			if changed {
				fn["arguments"] = string(marshal(newArgs))
			}
			kept = append(kept, tv)
		}
		if len(kept) == 0 {
			delete(m, "tool_calls")
			if fr, _ := c["finish_reason"].(string); fr == "tool_calls" {
				c["finish_reason"] = "stop"
			}
		} else {
			m["tool_calls"] = kept
		}
		if len(notes) > 0 {
			existing, _ := m["content"].(string)
			m["content"] = joinNotes(existing, notes)
		}
	}
}

func (g *guard) guardResponses(body map[string]any) {
	items := arr(body["output"])
	if items == nil {
		return
	}
	out := make([]any, 0, len(items))
	for _, iv := range items {
		it := obj(iv)
		if str(it, "type") != "function_call" {
			out = append(out, iv)
			continue
		}
		argsText := str(it, "arguments")
		ok, msg, newArgs, changed := g.call(str(it, "name"), parseArgs(argsText), argsText)
		if !ok {
			out = append(out, messageItem(msg))
			continue
		}
		if changed {
			it["arguments"] = string(marshal(newArgs))
		}
		out = append(out, iv)
	}
	body["output"] = out
}

func (g *guard) guardMessages(body map[string]any) {
	blocks := arr(body["content"])
	if blocks == nil {
		return
	}
	remaining := 0
	for i, bv := range blocks {
		b := obj(bv)
		if str(b, "type") != "tool_use" {
			continue
		}
		in := obj(b["input"])
		if in == nil {
			in = map[string]any{}
		}
		ok, msg, newArgs, changed := g.call(str(b, "name"), in, string(marshal(in)))
		if !ok {
			blocks[i] = map[string]any{"type": "text", "text": msg}
			continue
		}
		if changed {
			b["input"] = newArgs
		}
		remaining++
	}
	if sr, _ := body["stop_reason"].(string); sr == "tool_use" && remaining == 0 {
		body["stop_reason"] = "end_turn"
	}
}
