package mcpproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/approvals"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/jsonx"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/internal/vault"
)

type Scope struct {
	Agent   string
	Session string
	Server  string
}

type Corr struct {
	Method string
	Tool   string
}

type WaitHook func() func()

type Interceptor struct {
	Engine    *gateway.Engine
	pinMu     sync.Mutex
	rejMu     sync.Mutex
	rejected  map[string]time.Time
	PinExpiry time.Duration
}

func New(e *gateway.Engine) *Interceptor {
	i := &Interceptor{Engine: e, rejected: map[string]time.Time{}, PinExpiry: 24 * time.Hour}
	if e.Approvals != nil {
		e.Approvals.OnResolve("pin", i.onPin)
	}
	return i
}

type pinExtra struct {
	Tenant      string `json:"tenant"`
	Server      string `json:"server"`
	Tool        string `json:"tool"`
	Fingerprint string `json:"fingerprint"`
}

func (i *Interceptor) rejectKey(tenant, server, tool, fp string) string {
	return tenant + "/" + server + "/" + tool + "/" + fp
}

func (i *Interceptor) markRejected(tenant, server, tool, fp string) {
	i.rejMu.Lock()
	defer i.rejMu.Unlock()
	now := time.Now()
	for k, t := range i.rejected {
		if now.After(t) {
			delete(i.rejected, k)
		}
	}
	i.rejected[i.rejectKey(tenant, server, tool, fp)] = now.Add(time.Hour)
}

func (i *Interceptor) wasRejected(tenant, server, tool, fp string) bool {
	i.rejMu.Lock()
	defer i.rejMu.Unlock()
	t, ok := i.rejected[i.rejectKey(tenant, server, tool, fp)]
	return ok && time.Now().Before(t)
}

func (i *Interceptor) onPin(a *store.Approval) {
	var ex pinExtra
	if json.Unmarshal(a.Extra, &ex) != nil {
		return
	}
	i.pinMu.Lock()
	defer i.pinMu.Unlock()
	ctx := context.Background()
	pin, err := i.Engine.Store.GetPin(ctx, ex.Tenant, ex.Server, ex.Tool)
	if err != nil || pin.ApprovalID != a.ID {
		return
	}
	approve := a.Status == approvals.StatusApproved
	if approve && pin.PendingFingerprint == ex.Fingerprint {
		pin.Fingerprint, pin.Description, pin.Schema = pin.PendingFingerprint, pin.PendingDescription, pin.PendingSchema
		pin.Status = "approved"
		pin.ApprovedBy = a.DecidedBy
	} else {
		i.markRejected(ex.Tenant, ex.Server, ex.Tool, ex.Fingerprint)
		if pin.Fingerprint == "" {
			pin.Status = "rejected"
		} else {
			pin.Status = "approved"
		}
	}
	pin.PendingFingerprint, pin.PendingDescription, pin.PendingSchema, pin.PendingSince, pin.ApprovalID = "", "", nil, nil, ""
	if err := i.Engine.Store.PutPin(ctx, pin); err != nil {
		return
	}
	i.Engine.Emit(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: ex.Tenant, AgentID: a.AgentID, SessionID: a.SessionID, Type: "pin.decided", Message: fmt.Sprintf("%s/%s pin %s by %s", ex.Server, ex.Tool, pin.Status, a.DecidedBy)})
}

func (i *Interceptor) agent(sc Scope) *gateway.Agent { return i.Engine.Registry.Get(sc.Agent) }

func (i *Interceptor) base(ag *gateway.Agent, sc Scope) *core.Action {
	return &core.Action{AgentID: sc.Agent, SessionID: sc.Session, TenantID: ag.Manifest.Metadata.Tenant, Channel: core.ChannelMCP, Server: sc.Server}
}

func (i *Interceptor) pinMode(ag *gateway.Agent, server string) string {
	if s := ag.Manifest.FindServer(server); s != nil && s.Pinning != "" {
		return s.Pinning
	}
	return "tofu"
}

func (i *Interceptor) wait(out *gateway.Outcome, hook WaitHook, ctx context.Context) {
	if out.Decision.Verdict != core.VerdictHold || out.Decision.ApprovalID == "" {
		return
	}
	var stop func()
	if hook != nil {
		stop = hook()
	}
	i.Engine.Await(ctx, out)
	if stop != nil {
		stop()
	}
}

func (i *Interceptor) gateFinding(ag *gateway.Agent, sc Scope, tool string) []core.Finding {
	mode := i.pinMode(ag, sc.Server)
	if mode == "off" {
		return nil
	}
	pin, err := i.Engine.Store.GetPin(context.Background(), ag.Manifest.Metadata.Tenant, sc.Server, tool)
	mk := func(msg string) []core.Finding {
		return []core.Finding{{Detector: "mcp_pin", Category: core.CatPin, Rule: "pin_gate", Severity: 1, Confidence: 1, Message: msg, Min: core.VerdictBlock}}
	}
	if err != nil || pin == nil {
		if mode == "strict" {
			return mk("tool " + tool + " has no approved pin on server " + sc.Server)
		}
		return nil
	}
	if pin.PendingFingerprint != "" {
		return mk("tool " + tool + " definition changed on " + sc.Server + " and is pending approval")
	}
	if pin.Fingerprint == "" || pin.Status == "rejected" {
		return mk("tool " + tool + " on " + sc.Server + " was not approved")
	}
	return nil
}

func (i *Interceptor) Client(ctx context.Context, sc Scope, msg Message, hook WaitHook) (forward bool, reply Message, corr Corr) {
	method := Method(msg)
	corr.Method = method
	if !IsRequest(msg) {
		return true, nil, corr
	}
	ag := i.agent(sc)
	if ag == nil {
		return false, ErrorReply(msg, -32001, "unknown agent "+sc.Agent+": register a manifest first"), corr
	}
	if method != "tools/call" {
		return true, nil, corr
	}
	params := jsonx.Obj(msg["params"])
	name := jsonx.Str(params, "name")
	corr.Tool = name
	if name == "" {
		return true, nil, corr
	}
	args := jsonx.Obj(params["arguments"])
	argsText := string(jsonx.Marshal(args))
	callArgs := args
	restored := false
	pv := ag.Manifest.Spec.Privacy.RestoreInToolArgs
	if (pv == nil || *pv) && args != nil && vault.TokenRe.MatchString(argsText) && i.Engine.Vault != nil {
		if r, ok := i.Engine.Vault.RestoreAny(ag.Manifest.Metadata.Tenant, sc.Session, args).(map[string]any); ok {
			callArgs = r
			restored = true
		}
	}
	a := i.base(ag, sc)
	a.Kind = core.KindToolCall
	a.Tool = name
	a.Args = callArgs
	a.Meta = map[string]string{"channel": "mcp"}
	if len(callArgs) > 0 {
		a.Texts = []core.TextPart{{Source: core.SourceToolArgs, Text: string(jsonx.Marshal(callArgs))}}
	}
	a.Precomputed = i.gateFinding(ag, sc, name)
	out := i.Engine.Decide(ctx, a)
	i.wait(out, hook, ctx)
	if !out.Allowed {
		m := out.Message()
		if m == "" {
			m = "Rungated blocked tool call " + name
		}
		return false, ToolError(msg, m), corr
	}
	if restored {
		params["arguments"] = callArgs
	}
	return true, nil, corr
}

func schemaStrings(v any, depth int, out *[]string) {
	if depth > 12 || len(*out) > 200 {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if s, ok := x.(string); ok && (k == "description" || k == "title" || k == "default" || k == "pattern") {
				*out = append(*out, s)
				continue
			}
			schemaStrings(x, depth+1, out)
		}
	case []any:
		for _, x := range t {
			schemaStrings(x, depth+1, out)
		}
	}
}

func (i *Interceptor) Server(ctx context.Context, sc Scope, corr Corr, msg Message, hook WaitHook) Message {
	if !IsResponse(msg) {
		return msg
	}
	ag := i.agent(sc)
	if ag == nil {
		return msg
	}
	if _, bad := msg["error"]; bad {
		return msg
	}
	res := jsonx.Obj(msg["result"])
	if res == nil {
		return msg
	}
	switch corr.Method {
	case "tools/list":
		i.rewriteList(ctx, sc, ag, res)
		return msg
	case "tools/call":
		return i.scanResult(ctx, sc, ag, corr, msg, res, hook)
	case "resources/read":
		return i.scanContent(ctx, sc, ag, core.KindMCPResource, core.SourceFile, msg, res, hook)
	case "prompts/get":
		return i.scanContent(ctx, sc, ag, core.KindMCPPrompt, core.SourceRAG, msg, res, hook)
	}
	return msg
}

func leaves(v any, path string, src core.Source, out *[]core.TextPart) {
	if len(*out) >= 400 {
		return
	}
	switch t := v.(type) {
	case string:
		if t != "" {
			*out = append(*out, core.TextPart{Source: src, Text: t, Path: path})
		}
	case map[string]any:
		for k, x := range t {
			leaves(x, path+"."+k, src, out)
		}
	case []any:
		for n, x := range t {
			leaves(x, fmt.Sprintf("%s.%d", path, n), src, out)
		}
	}
}

func (i *Interceptor) apply(msg Message, a *core.Action, out *gateway.Outcome) {
	if len(out.Texts) != len(a.Texts) {
		return
	}
	for n, p := range a.Texts {
		if out.Texts[n] != p.Text && p.Path != "" {
			jsonx.SetPath(msg, p.Path, out.Texts[n])
		}
	}
}

func (i *Interceptor) scanResult(ctx context.Context, sc Scope, ag *gateway.Agent, corr Corr, msg Message, res map[string]any, hook WaitHook) Message {
	a := i.base(ag, sc)
	a.Kind = core.KindToolResult
	a.Tool = corr.Tool
	leaves(res["content"], "result.content", core.SourceToolResult, &a.Texts)
	if sc, ok := res["structuredContent"]; ok {
		leaves(sc, "result.structuredContent", core.SourceToolResult, &a.Texts)
	}
	if len(a.Texts) == 0 {
		return msg
	}
	out := i.Engine.Decide(ctx, a)
	i.wait(out, hook, ctx)
	if !out.Allowed {
		m := out.Message()
		if m == "" {
			m = "Rungated blocked the result of " + corr.Tool
		}
		return ToolError(msg, m)
	}
	i.apply(msg, a, out)
	return msg
}

func (i *Interceptor) scanContent(ctx context.Context, sc Scope, ag *gateway.Agent, kind core.Kind, src core.Source, msg Message, res map[string]any, hook WaitHook) Message {
	a := i.base(ag, sc)
	a.Kind = kind
	key := "contents"
	if kind == core.KindMCPPrompt {
		key = "messages"
	}
	leaves(res[key], "result."+key, src, &a.Texts)
	n := 0
	kept := a.Texts[:0]
	for _, p := range a.Texts {
		if strings.HasSuffix(p.Path, ".text") {
			kept = append(kept, p)
			n++
		}
	}
	a.Texts = kept
	if n == 0 {
		return msg
	}
	out := i.Engine.Decide(ctx, a)
	i.wait(out, hook, ctx)
	if !out.Allowed {
		m := out.Message()
		if m == "" {
			m = "Rungated blocked this " + string(kind)
		}
		return ErrorReply(msg, -32001, m)
	}
	i.apply(msg, a, out)
	return msg
}

func (i *Interceptor) rewriteList(ctx context.Context, sc Scope, ag *gateway.Agent, res map[string]any) {
	tools := jsonx.Arr(res["tools"])
	if tools == nil {
		return
	}
	mode := i.pinMode(ag, sc.Server)
	keep := make([]any, 0, len(tools))
	for _, t := range tools {
		tm := jsonx.Obj(t)
		name := jsonx.Str(tm, "name")
		if name == "" {
			continue
		}
		if ag.Manifest.FindTool(sc.Server, name) == nil {
			i.Engine.Emit(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: ag.Manifest.Metadata.Tenant, AgentID: sc.Agent, SessionID: sc.Session, Type: "mcp.tool_hidden", Message: fmt.Sprintf("%s/%s hidden: not declared in manifest", sc.Server, name)})
			continue
		}
		if i.visible(ctx, sc, ag, mode, tm) {
			keep = append(keep, t)
		}
	}
	res["tools"] = keep
}

func (i *Interceptor) visible(ctx context.Context, sc Scope, ag *gateway.Agent, mode string, tm map[string]any) bool {
	name := jsonx.Str(tm, "name")
	desc := jsonx.Str(tm, "description")
	schema := json.RawMessage(jsonx.Marshal(tm["inputSchema"]))
	if tm["inputSchema"] == nil {
		schema = json.RawMessage("null")
	}
	fp := detect.ToolFingerprint(name, desc, schema)
	tenant := ag.Manifest.Metadata.Tenant

	i.pinMu.Lock()
	defer i.pinMu.Unlock()
	st := i.Engine.Store
	pin, _ := st.GetPin(ctx, tenant, sc.Server, name)
	if pin != nil && mode != "off" {
		if pin.Status == "approved" && pin.Fingerprint == fp {
			if pin.PendingFingerprint != "" {
				old := pin.ApprovalID
				pin.PendingFingerprint, pin.PendingDescription, pin.PendingSchema, pin.PendingSince, pin.ApprovalID = "", "", nil, nil, ""
				st.PutPin(ctx, pin)
				if old != "" {
					i.supersede(ctx, old)
				}
			}
			return true
		}
		if pin.PendingFingerprint == fp {
			return false
		}
	}
	if mode != "off" && i.wasRejected(tenant, sc.Server, name, fp) {
		return false
	}

	a := i.base(ag, sc)
	a.Kind = core.KindMCPToolsList
	a.Tool = name
	a.Texts = []core.TextPart{{Source: core.SourceToolDesc, Text: name + ": " + desc}}
	var ss []string
	if tm["inputSchema"] != nil {
		schemaStrings(tm["inputSchema"], 0, &ss)
	}
	for _, s := range ss {
		a.Texts = append(a.Texts, core.TextPart{Source: core.SourceToolDesc, Text: s})
	}
	changed := pin != nil && pin.Status == "approved" && pin.Fingerprint != "" && pin.Fingerprint != fp
	firstSight := pin == nil || pin.Fingerprint == ""
	if mode != "off" {
		if shadow := i.shadow(ctx, tenant, sc.Server, name); shadow != "" {
			a.Precomputed = append(a.Precomputed, core.Finding{Detector: "mcp_pin", Category: core.CatPin, Rule: "tool_shadowing", Severity: 0.85, Confidence: 0.9, Message: shadow, Min: core.VerdictHold})
		}
		if changed {
			a.Precomputed = append(a.Precomputed, core.Finding{Detector: "mcp_pin", Category: core.CatPin, Rule: "definition_changed", Severity: 1, Confidence: 1, Message: "tool " + name + " definition changed since it was approved (possible rug pull)", Min: core.VerdictHold})
		} else if firstSight && mode == "strict" {
			a.Precomputed = append(a.Precomputed, core.Finding{Detector: "mcp_pin", Category: core.CatPin, Rule: "unpinned", Severity: 0.5, Confidence: 1, Message: "tool " + name + " is not pinned and server pinning is strict", Min: core.VerdictHold})
		}
	}
	out := i.Engine.Decide(ctx, a)
	d := out.Decision
	clean := d.Verdict.Rank() <= core.VerdictRedact.Rank()
	if mode == "off" {
		return clean
	}
	if d.Verdict == core.VerdictContain {
		return false
	}
	now := time.Now().UTC()
	if firstSight && clean && mode == "tofu" {
		np := &store.Pin{Tenant: tenant, Server: sc.Server, Tool: name, Fingerprint: fp, Description: desc, Schema: schema, Status: "approved", FirstSeen: now, ApprovedBy: "tofu"}
		if pin != nil {
			np.FirstSeen = pin.FirstSeen
		}
		st.PutPin(ctx, np)
		i.Engine.Emit(core.Event{ID: core.NewID("evt"), Time: now, TenantID: tenant, AgentID: sc.Agent, SessionID: sc.Session, Type: "pin.created", Message: sc.Server + "/" + name + " pinned on first use"})
		return true
	}
	np := pin
	if np == nil {
		np = &store.Pin{Tenant: tenant, Server: sc.Server, Tool: name, Status: "pending", FirstSeen: now}
	}
	old := np.ApprovalID
	np.PendingFingerprint, np.PendingDescription, np.PendingSchema, np.PendingSince = fp, desc, schema, &now
	np.ApprovalID = ""
	st.PutPin(ctx, np)
	if old != "" {
		i.supersede(ctx, old)
	}
	summary := fmt.Sprintf("%s/%s: new tool definition needs approval", sc.Server, name)
	if changed {
		summary = fmt.Sprintf("%s/%s: tool definition changed after approval", sc.Server, name)
	}
	if len(d.Reasons) > 0 {
		summary += ": " + d.Reasons[0]
	}
	ex, _ := json.Marshal(pinExtra{Tenant: tenant, Server: sc.Server, Tool: name, Fingerprint: fp})
	ap := &store.Approval{
		ID: core.NewID("apr"), Tenant: tenant, AgentID: sc.Agent, SessionID: sc.Session, Kind: "pin", Summary: summary, Decision: d,
		CreatedAt: now, ExpiresAt: now.Add(i.PinExpiry), OnTimeout: "deny", Extra: ex,
	}
	np.ApprovalID = ap.ID
	st.PutPin(ctx, np)
	if err := i.Engine.Approvals.Create(context.WithoutCancel(ctx), ap); err != nil {
		np.ApprovalID = ""
		st.PutPin(ctx, np)
	}
	return false
}

func (i *Interceptor) supersede(ctx context.Context, id string) {
	go i.Engine.Approvals.Decide(context.WithoutCancel(ctx), id, false, "rungated", "superseded by a newer tool definition")
}

func (i *Interceptor) shadow(ctx context.Context, tenant, server, tool string) string {
	pins, err := i.Engine.Store.ListPins(ctx, tenant)
	if err != nil {
		return ""
	}
	for _, p := range pins {
		if p.Tool == tool && p.Server != server && p.Status == "approved" && p.Fingerprint != "" {
			return fmt.Sprintf("tool %s on %s shadows the approved tool of the same name on %s", tool, server, p.Server)
		}
	}
	return ""
}
