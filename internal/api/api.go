package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/amareshhebbar/rungated/internal/approvals"
	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/contain"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/events"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/jsonx"
	"github.com/amareshhebbar/rungated/internal/metrics"
	"github.com/amareshhebbar/rungated/internal/policy"
	"github.com/amareshhebbar/rungated/internal/sse"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/internal/vault"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type Deps struct {
	Version   string
	Registry  *gateway.Registry
	Engine    *gateway.Engine
	Policy    *policy.Engine
	Store     store.Store
	Bus       events.Bus
	Approvals *approvals.Service
	Contain   *contain.Service
	Vault     *vault.Vault
	Keys      *auth.APIKeys
	Resolver  *auth.Resolver
	Metrics   *metrics.Metrics
	Edition   string
	Ext       map[string]any
	CORS      []string
	Static    http.Handler
}

type API struct {
	d Deps
}

var routeHooks []func(*API, *http.ServeMux)

func New(d Deps) *API { return &API{d: d} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func failErr(w http.ResponseWriter, err error) {
	var ve *manifest.ValidationError
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not found")
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": ve.Error(), "errors": ve.Errors})
	default:
		fail(w, http.StatusBadRequest, err.Error())
	}
}

type handler func(w http.ResponseWriter, r *http.Request, p *auth.Principal)

func (a *API) guard(action string, h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.d.Keys.Authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="rungated"`)
			fail(w, http.StatusUnauthorized, "missing or invalid api key")
			return
		}
		if !p.Can(action) {
			fail(w, http.StatusForbidden, "role "+string(p.Role)+" cannot "+action)
			return
		}
		h(w, r, p)
	}
}

func readBody(w http.ResponseWriter, r *http.Request, max int64) ([]byte, bool) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, max))
	if err != nil {
		fail(w, http.StatusRequestEntityTooLarge, "request body too large")
		return nil, false
	}
	return b, true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	b, ok := readBody(w, r, 4<<20)
	if !ok {
		return false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return true
	}
	if err := json.Unmarshal(b, v); err != nil {
		fail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", a.health)
	mux.HandleFunc("GET /v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"version": a.d.Version, "edition": a.d.Edition, "go": runtime.Version()})
	})
	mux.HandleFunc("GET /metrics", a.guard("read", func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		a.d.Metrics.Write(w)
	}))
	mux.HandleFunc("GET /v1/stats", a.guard("read", a.stats))

	mux.HandleFunc("GET /v1/agents", a.guard("read", a.listAgents))
	mux.HandleFunc("POST /v1/agents", a.guard("register", a.registerAgent))
	mux.HandleFunc("GET /v1/agents/{id}", a.guard("read", a.getAgent))
	mux.HandleFunc("DELETE /v1/agents/{id}", a.guard("admin", a.deleteAgent))
	mux.HandleFunc("PUT /v1/agents/{id}/mode", a.guard("contain", a.setMode))
	mux.HandleFunc("POST /v1/agents/{id}/token", a.issueToken)

	mux.HandleFunc("POST /v1/decide", a.decide)
	mux.HandleFunc("POST /v1/restore", a.restore)

	mux.HandleFunc("GET /v1/events", a.guard("read", a.listEvents))
	mux.HandleFunc("GET /v1/events/stream", a.guard("read", a.streamEvents))
	mux.HandleFunc("POST /v1/events/{id}/feedback", a.guard("approve", a.eventFeedback))
	mux.HandleFunc("POST /v1/feedback", a.guard("approve", a.postFeedback))
	mux.HandleFunc("GET /v1/feedback", a.guard("read", a.listFeedback))

	mux.HandleFunc("GET /v1/incidents", a.guard("read", a.listIncidents))
	mux.HandleFunc("GET /v1/incidents/{id}", a.guard("read", a.getIncident))
	mux.HandleFunc("POST /v1/incidents/{id}/resolve", a.guard("approve", a.resolveIncident))

	mux.HandleFunc("GET /v1/approvals", a.guard("read", a.listApprovals))
	mux.HandleFunc("GET /v1/approvals/{id}", a.guard("read", a.getApproval))
	mux.HandleFunc("POST /v1/approvals/{id}/approve", a.guard("approve", a.decideApproval(true)))
	mux.HandleFunc("POST /v1/approvals/{id}/deny", a.guard("approve", a.decideApproval(false)))

	mux.HandleFunc("GET /v1/containments", a.guard("read", a.listContainments))
	mux.HandleFunc("POST /v1/contain", a.guard("contain", a.containAgent))
	mux.HandleFunc("POST /v1/release", a.guard("contain", a.release))

	mux.HandleFunc("GET /v1/policies", a.guard("read", a.listPolicies))
	mux.HandleFunc("POST /v1/policies/validate", a.guard("read", a.validatePolicy))
	mux.HandleFunc("GET /v1/policies/{tenant}", a.guard("read", a.getPolicy))
	mux.HandleFunc("PUT /v1/policies/{tenant}", a.guard("policy", a.putPolicy))
	mux.HandleFunc("GET /v1/policies/{tenant}/history", a.guard("read", a.policyHistory))

	mux.HandleFunc("GET /v1/pins", a.guard("read", a.listPins))
	mux.HandleFunc("POST /v1/pins/{tenant}/{server}/{tool}/approve", a.guard("approve", a.decidePin(true)))
	mux.HandleFunc("POST /v1/pins/{tenant}/{server}/{tool}/reject", a.guard("approve", a.decidePin(false)))

	mux.HandleFunc("GET /v1/audit", a.guard("audit", a.listAudit))
	mux.HandleFunc("GET /v1/audit/verify", a.guard("audit", a.verifyAudit))

	for _, f := range routeHooks {
		f(a, mux)
	}

	mux.HandleFunc("POST /v1/manifests/validate", a.guard("read", a.validateManifest))
	mux.HandleFunc("POST /v1/manifests/compile", a.guard("read", a.compileManifest))

	if a.d.Static != nil {
		mux.Handle("/", a.d.Static)
	}
	return a.cors(mux)
}

func (a *API) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range a.d.CORS {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && (allowed["*"] || allowed[origin]) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Rungated-Agent, X-Rungated-Key, X-Rungated-Token, X-Rungated-Session")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", 200
	storeStatus := "ok"
	if err := a.d.Store.Ping(ctx); err != nil {
		status, code, storeStatus = "degraded", 503, err.Error()
	}
	writeJSON(w, code, map[string]any{"status": status, "store": storeStatus, "time": time.Now().UTC()})
}

func (a *API) stats(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	pending := 0
	if list, err := a.d.Store.ListApprovals(r.Context(), store.ApprovalFilter{Tenant: p.Tenant, Status: approvals.StatusPending, Limit: 10000}); err == nil {
		pending = len(list)
	}
	writeJSON(w, 200, map[string]any{
		"decisions":          a.d.Engine.Stats.Snapshot(),
		"vault":              a.d.Vault.Stats(),
		"active_containment": len(a.d.Contain.Active()),
		"pending_approvals":  pending,
		"agents":             len(a.d.Registry.List(p.Tenant)),
	})
}

func (a *API) agentSummary(ag *gateway.Agent) map[string]any {
	m := ag.Manifest
	rec := ag.Record
	seen := a.d.Registry.LastSeen(m.Metadata.ID)
	if seen.IsZero() {
		seen = rec.LastSeen
	}
	out := map[string]any{
		"id": m.Metadata.ID, "name": m.Metadata.Name, "tenant": m.Metadata.Tenant, "owner": m.Metadata.Owner, "purpose": m.Metadata.Purpose,
		"risk_tier": m.Spec.RiskTier, "mode": m.Spec.Autonomy.Default, "mode_override": rec.ModeOverride, "effective_mode": ag.Mode(core.KindToolCall, ""),
		"status": rec.Status, "version": rec.Version, "tools": len(m.Spec.Tools), "mcp_servers": len(m.Spec.MCPServers),
		"created_at": rec.CreatedAt, "updated_at": rec.UpdatedAt,
	}
	if !seen.IsZero() {
		out["last_seen"] = seen
	}
	return out
}

func (a *API) agentVisible(p *auth.Principal, ag *gateway.Agent) bool {
	return p.Tenant == "" || p.Tenant == ag.Manifest.Metadata.Tenant
}

func (a *API) listAgents(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var out []map[string]any
	for _, ag := range a.d.Registry.List(p.Tenant) {
		out = append(out, a.agentSummary(ag))
	}
	if out == nil {
		out = []map[string]any{}
	}
	writeJSON(w, 200, map[string]any{"agents": out})
}

func (a *API) registerAgent(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	raw, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		failErr(w, err)
		return
	}
	if p.Tenant != "" && m.Metadata.Tenant != p.Tenant {
		fail(w, http.StatusForbidden, "manifest tenant is outside your scope")
		return
	}
	rotate := r.URL.Query().Get("rotate_key") == "true"
	res, err := a.d.Registry.Register(r.Context(), raw, rotate)
	if err != nil {
		failErr(w, err)
		return
	}
	a.d.Bus.Publish(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: m.Metadata.Tenant, AgentID: m.Metadata.ID, Type: "agent.registered", Message: "manifest version " + strconv.Itoa(res.Agent.Record.Version) + " registered by " + p.Name})
	out := map[string]any{"agent": a.agentSummary(res.Agent), "created": res.Created}
	if res.Key != "" {
		out["key"] = res.Key
	}
	status := 200
	if res.Created {
		status = 201
	}
	writeJSON(w, status, out)
}

func (a *API) getAgent(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	ag := a.d.Registry.Get(r.PathValue("id"))
	if ag == nil || !a.agentVisible(p, ag) {
		fail(w, 404, "not found")
		return
	}
	cedar, _ := a.d.Policy.Source(ag.Manifest.Metadata.ID)
	usage := map[string]float64{}
	if a.d.Engine.Detect.Limiter != nil {
		usage = a.d.Engine.Detect.Limiter.Snapshot(ag.Manifest.Metadata.ID)
	}
	writeJSON(w, 200, map[string]any{"agent": a.agentSummary(ag), "manifest": ag.Record.Manifest, "manifest_parsed": ag.Manifest, "cedar": cedar, "usage": usage})
}

func (a *API) deleteAgent(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	id := r.PathValue("id")
	ag := a.d.Registry.Get(id)
	if ag == nil || !a.agentVisible(p, ag) {
		fail(w, 404, "not found")
		return
	}
	if err := a.d.Registry.Delete(r.Context(), id); err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"deleted": id})
}

func (a *API) setMode(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	id := r.PathValue("id")
	ag := a.d.Registry.Get(id)
	if ag == nil || !a.agentVisible(p, ag) {
		fail(w, 404, "not found")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := a.d.Registry.SetMode(r.Context(), id, core.Mode(body.Mode)); err != nil {
		failErr(w, err)
		return
	}
	a.d.Bus.Publish(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: ag.Manifest.Metadata.Tenant, AgentID: id, Type: "agent.mode_changed", Message: "mode override set to '" + body.Mode + "' by " + p.Name})
	writeJSON(w, 200, map[string]any{"agent": a.agentSummary(a.d.Registry.Get(id))})
}

func (a *API) issueToken(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ag := a.d.Registry.Get(id)
	if ag == nil {
		fail(w, 404, "not found")
		return
	}
	authorized := false
	if key := r.Header.Get("X-Rungated-Key"); key != "" {
		authorized = r.Header.Get("X-Rungated-Agent") == id && a.d.Registry.CheckKey(id, key)
	} else if p, ok := a.d.Keys.Authenticate(r); ok && p.Can("register") && (p.Tenant == "" || p.Tenant == ag.Manifest.Metadata.Tenant) {
		authorized = true
	}
	if !authorized {
		fail(w, http.StatusUnauthorized, "agent key or admin api key required")
		return
	}
	var body struct {
		SessionID  string `json:"session_id"`
		TTLSeconds int    `json:"ttl_seconds"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if _, contained := a.d.Contain.IsContained(id, body.SessionID); contained {
		fail(w, http.StatusForbidden, "agent is contained")
		return
	}
	tok, c := a.d.Contain.Issuer().Issue(id, ag.Manifest.Metadata.Tenant, body.SessionID, time.Duration(body.TTLSeconds)*time.Second)
	writeJSON(w, 200, map[string]any{"token": tok, "agent_id": id, "session_id": body.SessionID, "expires_at": time.Unix(c.Expires, 0).UTC(), "token_id": c.ID})
}

type decideReq struct {
	AgentID   string            `json:"agent_id"`
	SessionID string            `json:"session_id"`
	Kind      core.Kind         `json:"kind"`
	Channel   core.Channel      `json:"channel"`
	Tool      string            `json:"tool"`
	Server    string            `json:"server"`
	Method    string            `json:"method"`
	Args      map[string]any    `json:"args"`
	Texts     []core.TextPart   `json:"texts"`
	Model     string            `json:"model"`
	Provider  string            `json:"provider"`
	Host      string            `json:"host"`
	URL       string            `json:"url"`
	Bytes     int               `json:"bytes"`
	Meta      map[string]string `json:"meta"`
}

var validKinds = map[core.Kind]bool{
	core.KindLLMRequest: true, core.KindLLMResponse: true, core.KindToolCall: true, core.KindToolResult: true,
	core.KindMCPToolsList: true, core.KindMCPResource: true, core.KindMCPPrompt: true, core.KindEgressRequest: true, core.KindEgressResponse: true,
}

func (a *API) identity(r *http.Request, claimed string) (*auth.Identity, error) {
	id, err := a.d.Resolver.Resolve(r)
	if err == nil {
		return id, nil
	}
	if claimed != "" {
		if p, ok := a.d.Keys.Authenticate(r); ok && !a.d.Keys.Disabled && p.Can("decide") {
			return &auth.Identity{AgentID: claimed, SessionID: r.Header.Get("X-Rungated-Session"), Method: "api_key"}, nil
		}
		if a.d.Keys.Disabled {
			return &auth.Identity{AgentID: claimed, SessionID: r.Header.Get("X-Rungated-Session"), Method: "dev"}, nil
		}
	}
	return nil, err
}

func findingView(fs []core.Finding) []map[string]any {
	out := make([]map[string]any, 0, len(fs))
	for _, f := range fs {
		m := map[string]any{"detector": f.Detector, "tier": f.Tier, "category": f.Category, "rule": f.Rule, "severity": f.Severity, "confidence": f.Confidence, "message": f.Message}
		if f.DataClass != "" {
			m["data_class"] = f.DataClass
		}
		if f.Min != "" {
			m["min_verdict"] = f.Min
		}
		out = append(out, m)
	}
	return out
}

func (a *API) decide(w http.ResponseWriter, r *http.Request) {
	var req decideReq
	if !decodeBody(w, r, &req) {
		return
	}
	id, err := a.identity(r, req.AgentID)
	if err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	if req.AgentID != "" && req.AgentID != id.AgentID && id.Method != "api_key" && id.Method != "dev" {
		fail(w, http.StatusForbidden, "agent_id does not match credentials")
		return
	}
	if req.SessionID != "" && id.SessionID == "" {
		id.SessionID = req.SessionID
	}
	if req.Kind == "" && req.Tool != "" {
		req.Kind = core.KindToolCall
	}
	if !validKinds[req.Kind] {
		fail(w, http.StatusBadRequest, "kind must be one of llm.request, llm.response, tool.call, tool.result, mcp.tools_list, mcp.resource, mcp.prompt, egress.request, egress.response")
		return
	}
	if req.Channel == "" {
		req.Channel = core.ChannelSDK
	}
	act := &core.Action{AgentID: id.AgentID, SessionID: id.SessionID, Channel: req.Channel, Kind: req.Kind, Provider: req.Provider, Model: req.Model, Server: req.Server, Tool: req.Tool, Method: req.Method, Args: req.Args, Host: req.Host, URL: req.URL, Texts: req.Texts, Bytes: req.Bytes, Meta: req.Meta}
	if act.Kind == core.KindToolCall && len(act.Args) > 0 {
		hasArgs := false
		for _, p := range act.Texts {
			if p.Source == core.SourceToolArgs {
				hasArgs = true
				break
			}
		}
		if !hasArgs {
			act.Texts = append(act.Texts, core.TextPart{Source: core.SourceToolArgs, Text: string(jsonx.Marshal(act.Args))})
		}
	}
	if act.Host == "" && act.URL != "" {
		act.Host = hostOf(act.URL)
	}
	if act.Tool != "" && act.Kind == core.KindToolCall && a.d.Vault != nil {
		if ag := a.d.Registry.Get(id.AgentID); ag != nil {
			act.TenantID = ag.Manifest.Metadata.Tenant
		}
		if act.Args != nil {
			if rm, ok := a.d.Vault.RestoreAny(act.TenantID, act.SessionID, act.Args).(map[string]any); ok && vaultTokens(act.Args) {
				act.Args = rm
			}
		}
	}
	out := a.d.Engine.Decide(r.Context(), act)
	if out.Decision.Verdict == core.VerdictHold && r.URL.Query().Get("wait") == "true" {
		a.d.Engine.Await(r.Context(), out)
	}
	d := out.Decision
	resp := map[string]any{
		"action_id": act.ID, "verdict": d.Verdict, "computed": d.Computed, "allowed": out.Allowed, "score": d.Score, "mode": d.Mode,
		"reasons": d.Reasons, "findings": findingView(d.Findings), "latency_us": d.LatencyMicros,
	}
	if d.ApprovalID != "" {
		resp["approval_id"] = d.ApprovalID
		resp["approval_status"] = d.ApprovalStatus
	}
	if d.IncidentID != "" {
		resp["incident_id"] = d.IncidentID
	}
	if out.Texts != nil {
		resp["redacted_texts"] = out.Texts
	}
	if len(d.Redactions) > 0 {
		resp["redactions"] = d.Redactions
	}
	if msg := out.Message(); msg != "" {
		resp["message"] = msg
	}
	if out.Allowed && act.Kind == core.KindToolCall && act.Args != nil {
		resp["args"] = act.Args
	}
	status := 200
	writeJSON(w, status, resp)
}

func vaultTokens(args map[string]any) bool {
	b, _ := json.Marshal(args)
	return vault.TokenRe.Match(b)
}

func hostOf(u string) string {
	s := u
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "]") {
		s = s[:i]
	}
	return strings.ToLower(strings.Trim(s, "[]"))
}

func (a *API) restore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
		Value     any    `json:"value"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	id, err := a.identity(r, req.AgentID)
	if err != nil {
		fail(w, http.StatusUnauthorized, err.Error())
		return
	}
	sid := req.SessionID
	if sid == "" {
		sid = id.SessionID
	}
	tenant := ""
	if ag := a.d.Registry.Get(id.AgentID); ag != nil {
		tenant = ag.Manifest.Metadata.Tenant
	}
	if req.Value != nil {
		writeJSON(w, 200, map[string]any{"value": a.d.Vault.RestoreAny(tenant, sid, req.Value)})
		return
	}
	writeJSON(w, 200, map[string]any{"text": a.d.Vault.Restore(tenant, sid, req.Text)})
}

func atoiDef(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n
	}
	return def
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.UnixMilli(n)
	}
	return time.Time{}
}

func (a *API) listEvents(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	q := r.URL.Query()
	lim := atoiDef(q.Get("limit"), 100)
	if lim > 1000 {
		lim = 1000
	}
	f := store.EventFilter{Tenant: p.Tenant, AgentID: q.Get("agent"), SessionID: q.Get("session"), MinVerdict: q.Get("min_verdict"), Type: q.Get("type"), Since: parseTime(q.Get("since")), Before: parseTime(q.Get("before")), Limit: lim}
	evs, err := a.d.Store.QueryEvents(r.Context(), f)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if evs == nil {
		evs = []core.Event{}
	}
	writeJSON(w, 200, map[string]any{"events": evs, "count": len(evs)})
}

func eventMatches(e core.Event, tenant, agent, session, minV, typ string) bool {
	if tenant != "" && e.TenantID != tenant {
		return false
	}
	if agent != "" && e.AgentID != agent {
		return false
	}
	if session != "" && e.SessionID != session {
		return false
	}
	if typ != "" && e.Type != typ {
		return false
	}
	if minV != "" {
		if e.Decision == nil || e.Decision.Verdict.Rank() < core.Verdict(minV).Rank() {
			return false
		}
	}
	return true
}

func (a *API) streamEvents(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	if _, ok := w.(http.Flusher); !ok {
		fail(w, 500, "streaming unsupported")
		return
	}
	q := r.URL.Query()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	sw := sse.NewWriter(w)
	ch, cancel := a.d.Bus.Subscribe(512)
	defer cancel()
	sw.Comment("connected")
	hb := time.NewTicker(15 * time.Second)
	defer hb.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-hb.C:
			if sw.Comment("heartbeat") != nil {
				return
			}
		case e, ok := <-ch:
			if !ok {
				return
			}
			if !eventMatches(e, p.Tenant, q.Get("agent"), q.Get("session"), q.Get("min_verdict"), q.Get("type")) {
				continue
			}
			b, _ := json.Marshal(e)
			if sw.Write(&sse.Event{Event: e.Type, Data: string(b), ID: e.ID}) != nil {
				return
			}
		}
	}
}

func (a *API) listIncidents(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	q := r.URL.Query()
	list, err := a.d.Store.ListIncidents(r.Context(), store.IncidentFilter{Tenant: p.Tenant, Status: q.Get("status"), AgentID: q.Get("agent"), Limit: atoiDef(q.Get("limit"), 100)})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Incident{}
	}
	writeJSON(w, 200, map[string]any{"incidents": list})
}

func (a *API) getIncident(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	inc, err := a.d.Store.GetIncident(r.Context(), r.PathValue("id"))
	if err != nil || (p.Tenant != "" && inc.Tenant != p.Tenant) {
		fail(w, 404, "not found")
		return
	}
	evs, _ := a.d.Store.QueryEvents(r.Context(), store.EventFilter{IDs: inc.EventIDs, Limit: 1000})
	if evs == nil {
		evs = []core.Event{}
	}
	writeJSON(w, 200, map[string]any{"incident": inc, "events": evs})
}

func (a *API) resolveIncident(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	inc, err := a.d.Store.GetIncident(r.Context(), r.PathValue("id"))
	if err != nil || (p.Tenant != "" && inc.Tenant != p.Tenant) {
		fail(w, 404, "not found")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	inc.Status = "resolved"
	inc.UpdatedAt = time.Now().UTC()
	note := "resolved by " + p.Name
	if body.Note != "" {
		note += ": " + body.Note
	}
	inc.Notes = append(inc.Notes, inc.UpdatedAt.Format(time.RFC3339)+" "+note)
	if err := a.d.Store.PutIncident(r.Context(), inc); err != nil {
		fail(w, 500, err.Error())
		return
	}
	a.d.Bus.Publish(core.Event{ID: core.NewID("evt"), Time: inc.UpdatedAt, TenantID: inc.Tenant, AgentID: inc.AgentID, SessionID: inc.SessionID, Type: "incident.resolved", Message: note, Data: map[string]any{"incident_id": inc.ID}})
	writeJSON(w, 200, map[string]any{"incident": inc})
}

func (a *API) listApprovals(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	q := r.URL.Query()
	list, err := a.d.Store.ListApprovals(r.Context(), store.ApprovalFilter{Tenant: p.Tenant, Status: q.Get("status"), AgentID: q.Get("agent"), Limit: atoiDef(q.Get("limit"), 100)})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Approval{}
	}
	writeJSON(w, 200, map[string]any{"approvals": list})
}

func (a *API) getApproval(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	ap, err := a.d.Store.GetApproval(r.Context(), r.PathValue("id"))
	if err != nil || (p.Tenant != "" && ap.Tenant != p.Tenant) {
		fail(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]any{"approval": ap})
}

func (a *API) decideApproval(approve bool) handler {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id := r.PathValue("id")
		ap, err := a.d.Store.GetApproval(r.Context(), id)
		if err != nil || (p.Tenant != "" && ap.Tenant != p.Tenant) {
			fail(w, 404, "not found")
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		res, err := a.d.Approvals.Decide(r.Context(), id, approve, p.Name, body.Reason)
		if err != nil {
			if errors.Is(err, approvals.ErrNotPending) {
				writeJSON(w, http.StatusConflict, map[string]any{"error": "approval is not pending", "approval": res})
				return
			}
			failErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"approval": res})
	}
}

func (a *API) listContainments(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	active := r.URL.Query().Get("active") != "false"
	list, err := a.d.Store.ListContainments(r.Context(), active)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	out := make([]*store.Containment, 0, len(list))
	for _, c := range list {
		if p.Tenant == "" || c.Tenant == p.Tenant {
			out = append(out, c)
		}
	}
	writeJSON(w, 200, map[string]any{"containments": out})
}

func (a *API) containAgent(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var body struct {
		AgentID   string `json:"agent_id"`
		SessionID string `json:"session_id"`
		Reason    string `json:"reason"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	ag := a.d.Registry.Get(body.AgentID)
	if ag == nil || !a.agentVisible(p, ag) {
		fail(w, 404, "agent not found")
		return
	}
	if body.Reason == "" {
		body.Reason = "manual containment"
	}
	c, err := a.d.Contain.Contain(r.Context(), ag.Manifest.Metadata.Tenant, body.AgentID, body.SessionID, body.Reason, p.Name, false, nil)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]any{"containment": c})
}

func (a *API) release(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var body struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	key := body.ID
	if key == "" {
		key = body.AgentID
	}
	if key == "" {
		fail(w, 400, "id or agent_id required")
		return
	}
	if p.Tenant != "" {
		allowed := false
		for _, c := range a.d.Contain.Active() {
			if (c.ID == key || c.AgentID == key) && c.Tenant == p.Tenant {
				allowed = true
			}
		}
		if !allowed {
			fail(w, 404, "not found")
			return
		}
	}
	c, err := a.d.Contain.Release(r.Context(), key, p.Name)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"containment": c})
}

func (a *API) listPolicies(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	list, err := a.d.Store.ListPolicies(r.Context())
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	out := make([]*store.PolicyRecord, 0, len(list))
	for _, pr := range list {
		if p.Tenant == "" || pr.Tenant == p.Tenant {
			out = append(out, pr)
		}
	}
	writeJSON(w, 200, map[string]any{"policies": out})
}

func (a *API) tenantOK(p *auth.Principal, tenant string) bool {
	return p.Tenant == "" || p.Tenant == tenant
}

func (a *API) getPolicy(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	t := r.PathValue("tenant")
	if !a.tenantOK(p, t) {
		fail(w, 404, "not found")
		return
	}
	rec, err := a.d.Store.GetPolicy(r.Context(), t)
	out := map[string]any{"tenant": t, "source": a.d.Policy.Custom(t)}
	if err == nil {
		out["version"] = rec.Version
		out["author"] = rec.Author
		out["updated_at"] = rec.UpdatedAt
	} else {
		out["version"] = 0
	}
	writeJSON(w, 200, out)
}

func (a *API) putPolicy(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	t := r.PathValue("tenant")
	if !a.tenantOK(p, t) {
		fail(w, 404, "not found")
		return
	}
	var body struct {
		Source string `json:"source"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := a.d.Policy.SetCustom(t, body.Source); err != nil {
		fail(w, 400, "invalid cedar policy: "+err.Error())
		return
	}
	rec, err := a.d.Store.PutPolicy(r.Context(), t, body.Source, p.Name)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	a.d.Bus.Publish(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: t, Type: "policy.updated", Message: fmt.Sprintf("tenant policy version %d saved by %s", rec.Version, p.Name)})
	writeJSON(w, 200, map[string]any{"policy": rec})
}

func (a *API) validatePolicy(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var body struct {
		Source string `json:"source"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if err := policy.ValidateCedar(body.Source); err != nil {
		writeJSON(w, 200, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true})
}

func (a *API) policyHistory(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	t := r.PathValue("tenant")
	if !a.tenantOK(p, t) {
		fail(w, 404, "not found")
		return
	}
	h, err := a.d.Store.PolicyHistory(r.Context(), t)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"history": h})
}

func (a *API) listPins(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	list, err := a.d.Store.ListPins(r.Context(), p.Tenant)
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if list == nil {
		list = []*store.Pin{}
	}
	writeJSON(w, 200, map[string]any{"pins": list})
}

func (a *API) decidePin(approve bool) handler {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		t, s, tool := r.PathValue("tenant"), r.PathValue("server"), r.PathValue("tool")
		if !a.tenantOK(p, t) {
			fail(w, 404, "not found")
			return
		}
		pin, err := a.d.Store.GetPin(r.Context(), t, s, tool)
		if err != nil {
			fail(w, 404, "not found")
			return
		}
		now := time.Now().UTC()
		if pin.ApprovalID != "" {
			if _, err := a.d.Approvals.Decide(r.Context(), pin.ApprovalID, approve, p.Name, "pin decision"); err != nil && !errors.Is(err, approvals.ErrNotPending) {
				fail(w, 500, err.Error())
				return
			}
			if got, err := a.d.Store.GetPin(r.Context(), t, s, tool); err == nil {
				writeJSON(w, 200, map[string]any{"pin": got})
				return
			}
		}
		if pin.PendingFingerprint != "" {
			if approve {
				pin.Fingerprint = pin.PendingFingerprint
				pin.Description = pin.PendingDescription
				pin.Schema = pin.PendingSchema
				pin.Status = "approved"
				pin.ApprovedBy = p.Name
			} else {
				pin.Status = "approved"
				if pin.Fingerprint == "" {
					pin.Status = "rejected"
				}
			}
			pin.PendingFingerprint, pin.PendingDescription, pin.PendingSchema, pin.PendingSince = "", "", nil, nil
		} else if approve {
			pin.Status = "approved"
			pin.ApprovedBy = p.Name
		} else {
			pin.Status = "rejected"
		}
		if err := a.d.Store.PutPin(r.Context(), pin); err != nil {
			fail(w, 500, err.Error())
			return
		}
		a.d.Bus.Publish(core.Event{ID: core.NewID("evt"), Time: now, TenantID: t, Type: "pin.decided", Message: fmt.Sprintf("%s/%s pin %s by %s", s, tool, pin.Status, p.Name)})
		writeJSON(w, 200, map[string]any{"pin": pin})
	}
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	q := r.URL.Query()
	from := uint64(1)
	if n, err := strconv.ParseUint(q.Get("from"), 10, 64); err == nil && n > 0 {
		from = n
	}
	recs, err := a.d.Store.ListAudit(r.Context(), from, atoiDef(q.Get("limit"), 100))
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	if recs == nil {
		recs = []*store.AuditRecord{}
	}
	writeJSON(w, 200, map[string]any{"records": recs})
}

func (a *API) verifyAudit(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	res := events.Verify(r.Context(), a.d.Store, r.URL.Query().Get("events") != "false")
	status := 200
	if !res.OK {
		status = http.StatusConflict
	}
	writeJSON(w, status, res)
}

func (a *API) validateManifest(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	raw, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		var ve *manifest.ValidationError
		if errors.As(err, &ve) {
			writeJSON(w, 200, map[string]any{"valid": false, "errors": ve.Errors})
			return
		}
		writeJSON(w, 200, map[string]any{"valid": false, "errors": []manifest.FieldError{{Field: "manifest", Message: err.Error()}}})
		return
	}
	writeJSON(w, 200, map[string]any{"valid": true, "agent_id": m.Metadata.ID, "tenant": m.Metadata.Tenant, "tools": len(m.Spec.Tools)})
}

func (a *API) compileManifest(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	raw, ok := readBody(w, r, 1<<20)
	if !ok {
		return
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		failErr(w, err)
		return
	}
	src, err := policy.Compile(m)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"cedar": src})
}
