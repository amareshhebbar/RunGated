package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/approvals"
	"github.com/amareshhebbar/rungated/internal/contain"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/events"
	"github.com/amareshhebbar/rungated/internal/policy"
	"github.com/amareshhebbar/rungated/internal/risk"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/internal/vault"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type Engine struct {
	Registry      *Registry
	Policy        *policy.Engine
	Detect        *detect.Engine
	Judge         detect.Judge
	Vault         *vault.Vault
	Approvals     *approvals.Service
	Contain       *contain.Service
	Bus           events.Bus
	Store         store.Store
	Incidents     *Incidents
	Stats         *Stats
	UnknownAgents string
	MaxStoredText int
	Observers     []func(*core.Action, *core.Decision)
	grantMu       sync.Mutex
	grants        map[string]time.Time
}

const grantTTL = 10 * time.Minute

func (e *Engine) grantKey(a *core.Action) string {
	args := a.Args
	if e.Vault != nil && args != nil {
		if r, ok := e.Vault.RestoreAny(a.TenantID, a.SessionID, args).(map[string]any); ok {
			args = r
		}
	}
	return a.AgentID + "\x00" + a.SessionID + "\x00" + detect.ArgsHash(a.Tool, args)
}

func (e *Engine) granted(a *core.Action) bool {
	k := e.grantKey(a)
	e.grantMu.Lock()
	defer e.grantMu.Unlock()
	exp, ok := e.grants[k]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(e.grants, k)
		return false
	}
	return true
}

func (e *Engine) grant(a *core.Action) {
	k := e.grantKey(a)
	e.grantMu.Lock()
	defer e.grantMu.Unlock()
	if e.grants == nil {
		e.grants = map[string]time.Time{}
	}
	now := time.Now()
	for gk, exp := range e.grants {
		if now.After(exp) {
			delete(e.grants, gk)
		}
	}
	e.grants[k] = now.Add(grantTTL)
}

type Outcome struct {
	Decision *core.Decision
	Action   *core.Action
	Agent    *Agent
	Allowed  bool
	Texts    []string
}

func policyAction(k core.Kind) string {
	switch k {
	case core.KindLLMRequest:
		return policy.ActionLLM
	case core.KindToolCall:
		return policy.ActionTool
	case core.KindEgressRequest:
		return policy.ActionEgress
	}
	return ""
}

func (e *Engine) emit(ev core.Event) {
	if e.Bus != nil {
		e.Bus.Publish(ev)
	}
}

func (e *Engine) Emit(ev core.Event) { e.emit(ev) }

func classesMatch(patterns []string, class string) bool {
	for _, p := range patterns {
		if manifest.ClassMatch(p, class) {
			return true
		}
	}
	return false
}

func (e *Engine) storedAction(a *core.Action, d *core.Decision, m *manifest.Manifest) *core.Action {
	cp := *a
	cp.Precomputed = nil
	if m != nil && m.Spec.Privacy.StoreContent != nil && !*m.Spec.Privacy.StoreContent {
		cp.Texts = nil
		cp.Args = nil
		return &cp
	}
	max := e.MaxStoredText
	if max <= 0 {
		max = 4000
	}
	masks := map[int][]core.Finding{}
	for _, f := range d.Findings {
		if f.Span != nil && f.Match != "" && f.Category != core.CatInjection {
			masks[f.Span.Part] = append(masks[f.Span.Part], f)
		}
	}
	cp.Texts = make([]core.TextPart, len(a.Texts))
	for i, p := range a.Texts {
		t := p.Text
		for _, f := range masks[i] {
			t = strings.ReplaceAll(t, f.Match, "[masked:"+f.DataClass+"]")
		}
		if len(t) > max {
			t = t[:max] + "...[truncated]"
		}
		cp.Texts[i] = core.TextPart{Source: p.Source, Text: t, Path: p.Path}
	}
	if len(cp.Texts) > 40 {
		cp.Texts = cp.Texts[len(cp.Texts)-40:]
	}
	if a.Args != nil {
		masked := map[string]any{}
		for k, v := range a.Args {
			if s, ok := v.(string); ok {
				for _, fs := range masks {
					for _, f := range fs {
						s = strings.ReplaceAll(s, f.Match, "[masked:"+f.DataClass+"]")
					}
				}
				if len(s) > max {
					s = s[:max] + "...[truncated]"
				}
				masked[k] = s
			} else {
				masked[k] = v
			}
		}
		cp.Args = masked
	}
	return &cp
}

func (e *Engine) Decide(ctx context.Context, a *core.Action) *Outcome {
	start := time.Now()
	if a.ID == "" {
		a.ID = core.NewID("act")
	}
	if a.Time.IsZero() {
		a.Time = start.UTC()
	}
	d := &core.Decision{ActionID: a.ID, PolicyAllowed: true}
	out := &Outcome{Decision: d, Action: a}
	ag := e.Registry.Get(a.AgentID)
	if ag == nil {
		d.Computed, d.Verdict, d.Mode = core.VerdictBlock, core.VerdictBlock, core.ModeAutoContain
		d.Score = 100
		d.PolicyAllowed = false
		d.Reasons = []string{"unknown agent " + a.AgentID + ": register a manifest first (rungated init --id " + a.AgentID + " writes one)"}
		d.Findings = []core.Finding{{Detector: "registry", Category: core.CatPolicy, Rule: "unknown_agent", Severity: 1, Confidence: 1, Message: d.Reasons[0], Min: core.VerdictBlock}}
		if e.UnknownAgents == "observe" {
			d.Verdict = core.VerdictAllow
			d.Mode = core.ModeObserve
		}
		d.LatencyMicros = time.Since(start).Microseconds()
		e.finish(ctx, out, nil)
		out.Allowed = d.Verdict == core.VerdictAllow
		return out
	}
	out.Agent = ag
	m := ag.Manifest
	a.TenantID = m.Metadata.Tenant
	e.Registry.Touch(a.AgentID)
	d.Mode = ag.Mode(a.Kind, a.Tool)

	if c, ok := e.Contain.IsContained(a.AgentID, a.SessionID); ok {
		d.Computed, d.Verdict = core.VerdictBlock, core.VerdictBlock
		d.Score = 100
		d.Reasons = []string{"agent is contained: " + c.Reason}
		d.Findings = []core.Finding{{Detector: "contain", Category: core.CatContain, Rule: "contained", Severity: 1, Confidence: 1, Message: d.Reasons[0], Min: core.VerdictBlock}}
		d.LatencyMicros = time.Since(start).Microseconds()
		e.finish(ctx, out, m)
		return out
	}

	det := e.Detect.Run(ctx, a, m)
	findings := append(det.Findings, a.Precomputed...)
	d.Degraded = det.Degraded
	privacyOn := m.Spec.Privacy.RedactBeforeModel != nil && *m.Spec.Privacy.RedactBeforeModel

	redactableFn := func(f core.Finding) bool {
		if f.DataClass == "untrusted.injection" {
			return true
		}
		switch a.Kind {
		case core.KindLLMRequest:
			return privacyOn && classesMatch(m.Spec.Privacy.RedactClasses, f.DataClass)
		case core.KindToolResult, core.KindEgressResponse, core.KindMCPResource:
			return classesMatch(m.Spec.Privacy.RedactClasses, f.DataClass)
		}
		return false
	}

	pa := policyAction(a.Kind)
	if pa != "" {
		var classes []string
		for _, f := range findings {
			if f.DataClass == "" || f.DataClass == "untrusted.injection" {
				continue
			}
			if f.Redactable && redactableFn(f) {
				continue
			}
			classes = append(classes, f.DataClass)
		}
		maxTok := 0
		if a.Meta != nil {
			fmt.Sscan(a.Meta["max_tokens"], &maxTok)
		}
		httpMethod := a.Method
		if a.Meta["tunnel"] == "1" && len(m.Spec.Egress.Methods) > 0 {
			httpMethod = m.Spec.Egress.Methods[0]
		}
		pr := e.Policy.Evaluate(policy.Request{
			AgentID: a.AgentID, Action: pa, Model: a.Model, Provider: a.Provider, MaxTokens: maxTok,
			Server: a.Server, Tool: a.Tool, Method: a.Method, Args: a.Args,
			Host: a.Host, HTTPMethod: httpMethod, DataClasses: classes,
		})
		d.PolicyAllowed = pr.Allowed
		d.PolicyReasons = pr.Reasons
		if !pr.Allowed {
			msg := "policy denied " + pa
			switch pa {
			case policy.ActionTool:
				msg = fmt.Sprintf("tool call %s/%s outside manifest scope", a.Server, a.Tool)
			case policy.ActionEgress:
				msg = "egress to " + a.Host + " not allowed by manifest"
			case policy.ActionLLM:
				msg = "model " + a.Model + " via " + a.Provider + " not allowed by manifest"
			}
			if pr.Explicit {
				msg += " (explicit forbid: " + strings.Join(pr.Reasons, ", ") + ")"
			}
			findings = append(findings, core.Finding{Detector: "policy", Tier: 0, Category: core.CatPolicy, Rule: "cedar", Severity: 0.9, Confidence: 1, Message: msg, Min: core.VerdictBlock})
		}
	}
	if a.Kind == core.KindToolCall {
		if t := m.FindTool(a.Server, a.Tool); t != nil && t.RequireApproval {
			findings = append(findings, core.Finding{Detector: "policy", Category: core.CatScope, Rule: "require_approval", Severity: 0.5, Confidence: 1, Message: "tool " + a.Tool + " requires human approval", Min: core.VerdictHold})
		}
	}
	if det.Degraded {
		d.FailMode = m.Spec.Risk.FailMode
		if m.Spec.Risk.FailMode == "closed" {
			findings = append(findings, core.Finding{Detector: "engine", Category: core.CatScope, Rule: "fail_closed", Severity: 0.5, Confidence: 1, Message: "detector unavailable (" + strings.Join(det.Errors, "; ") + "), failing closed", Min: core.VerdictHold})
		}
	}

	findings = append(findings, egressClassFindings(a, m, findings)...)
	r := risk.Evaluate(risk.Input{Findings: findings, Manifest: m, ActionClass: string(a.Kind), Tool: a.Tool, Redactable: redactableFn})
	d.Findings = findings
	d.Score = r.Score
	d.Computed = r.Verdict
	d.Reasons = r.Reasons
	if privacyOn && a.Kind == core.KindLLMRequest && len(r.ToRedact) > 0 && d.Computed.Rank() < core.VerdictRedact.Rank() {
		d.Computed = core.VerdictRedact
	}
	if len(r.ToRedact) > 0 && d.Computed.Rank() < core.VerdictRedact.Rank() && (a.Kind == core.KindToolResult || a.Kind == core.KindEgressResponse || a.Kind == core.KindMCPResource) {
		for _, f := range r.ToRedact {
			if f.DataClass == "untrusted.injection" {
				d.Computed = core.VerdictRedact
			}
		}
	}

	if d.Computed == core.VerdictHold && e.Judge != nil && m.Spec.Risk.JudgeOnHold != nil && *m.Spec.Risk.JudgeOnHold {
		jv, err := e.Judge.Judge(ctx, a, d, m.Metadata.Purpose)
		if err != nil {
			slog.Warn("judge failed", "err", err)
			d.JudgeReason = "judge unavailable: " + err.Error()
		} else {
			d.JudgeReason = fmt.Sprintf("%s (%.2f): %s", jv.Verdict, jv.Confidence, jv.Reason)
			d.Findings = append(d.Findings, core.Finding{Detector: "llm_judge", Tier: 3, Category: core.CatJudge, Rule: jv.Verdict, Severity: map[string]float64{"malicious": 0.95, "uncertain": 0.5, "benign": 0.05}[jv.Verdict], Confidence: jv.Confidence, Message: "LLM judge: " + jv.Verdict + ": " + jv.Reason})
			switch {
			case jv.Verdict == "malicious" && jv.Confidence >= 0.8:
				d.Computed = core.VerdictBlock
				d.Reasons = append([]string{"LLM judge confirmed malicious: " + jv.Reason}, d.Reasons...)
			case jv.Verdict == "benign" && jv.Confidence >= 0.8 && m.Spec.Risk.JudgeCanRelease:
				d.Computed = core.VerdictWarn
				d.Reasons = append([]string{"LLM judge released hold: " + jv.Reason}, d.Reasons...)
			}
		}
	}

	blast := "medium"
	if t := m.FindTool(a.Server, a.Tool); t != nil {
		blast = t.BlastRadius
	}
	v, note := risk.Enforce(d.Computed, d.Mode, blast, !d.PolicyAllowed)
	d.Verdict = v
	if a.Kind == core.KindMCPToolsList && d.Verdict == core.VerdictHold {
		d.Verdict = core.VerdictBlock
		note = "tool definition held for pin approval"
	}
	if note != "" {
		d.Reasons = append(d.Reasons, note)
	}

	if d.Verdict == core.VerdictRedact || ((d.Verdict == core.VerdictHold) && len(r.ToRedact) > 0) {
		out.Texts = e.applyRedactions(a, d, r.ToRedact)
	}
	if d.Verdict == core.VerdictHold && a.Kind == core.KindToolCall && e.granted(a) {
		d.Verdict = core.VerdictAllow
		d.Reasons = append(d.Reasons, "previously approved")
	}
	if d.Verdict == core.VerdictHold {
		e.hold(ctx, out, m)
	}
	d.LatencyMicros = time.Since(start).Microseconds()
	e.finish(ctx, out, m)
	out.Allowed = d.Verdict.Rank() <= core.VerdictRedact.Rank()
	return out
}

func (e *Engine) applyRedactions(a *core.Action, d *core.Decision, fs []core.Finding) []string {
	byPart := map[int][]vault.Span{}
	for _, f := range fs {
		if f.Span == nil {
			continue
		}
		byPart[f.Span.Part] = append(byPart[f.Span.Part], vault.Span{Start: f.Span.Start, End: f.Span.End, Class: f.DataClass})
	}
	if len(byPart) == 0 {
		return nil
	}
	texts := make([]string, len(a.Texts))
	for i, p := range a.Texts {
		texts[i] = p.Text
		spans := byPart[i]
		if len(spans) == 0 {
			continue
		}
		red, reds, err := e.Vault.Redact(a.TenantID, a.SessionID, i, p.Text, spans)
		if err != nil {
			slog.Error("redaction failed", "err", err)
			continue
		}
		texts[i] = red
		d.Redactions = append(d.Redactions, reds...)
	}
	return texts
}

func (e *Engine) finish(ctx context.Context, out *Outcome, m *manifest.Manifest) {
	a, d := out.Action, out.Decision
	if e.Stats != nil {
		e.Stats.Record(a, d)
	}
	for _, fn := range e.Observers {
		fn(a, d)
	}
	ev := core.Event{ID: core.NewID("evt"), Time: a.Time, TenantID: a.TenantID, AgentID: a.AgentID, SessionID: a.SessionID, Type: "decision", Action: e.storedAction(a, d, m)}
	dc := *d
	ev.Decision = &dc
	if e.Incidents != nil {
		d.IncidentID = e.Incidents.Record(ctx, ev)
		dc.IncidentID = d.IncidentID
	}
	e.emit(ev)
	if d.Verdict == core.VerdictContain {
		trigger := ev
		c, err := e.Contain.Contain(context.WithoutCancel(ctx), a.TenantID, a.AgentID, a.SessionID, strings.Join(d.Reasons, "; "), "rungated:auto", true, &trigger)
		if err != nil {
			slog.Error("containment failed", "err", err)
		} else if e.Incidents != nil {
			e.Incidents.Attach(ctx, d.IncidentID, "", "automatic containment "+c.ID, c.SnapshotRef)
		}
	}
}

func summarize(a *core.Action) string {
	switch {
	case a.Tool != "":
		return fmt.Sprintf("%s %s/%s", a.Kind, a.Server, a.Tool)
	case a.Host != "":
		return fmt.Sprintf("%s %s %s", a.Kind, a.Method, a.URL)
	case a.Model != "":
		return fmt.Sprintf("%s %s", a.Kind, a.Model)
	}
	return string(a.Kind)
}

func (e *Engine) hold(ctx context.Context, out *Outcome, m *manifest.Manifest) {
	a, d := out.Action, out.Decision
	ap := &store.Approval{
		ID: core.NewID("apr"), Tenant: a.TenantID, AgentID: a.AgentID, SessionID: a.SessionID, Kind: "action",
		Summary: summarize(a) + ": " + d.Summary(), Action: e.storedAction(a, d, m), Decision: d,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Duration(m.Spec.Escalation.TimeoutSeconds) * time.Second),
		OnTimeout: m.Spec.Escalation.OnTimeout,
	}
	if err := e.Approvals.Create(context.WithoutCancel(ctx), ap); err != nil {
		slog.Error("approval create failed", "err", err)
		d.Verdict = core.VerdictBlock
		return
	}
	d.ApprovalID = ap.ID
	d.ApprovalStatus = approvals.StatusPending
}

func (e *Engine) Await(ctx context.Context, out *Outcome) bool {
	d := out.Decision
	if d.Verdict != core.VerdictHold || d.ApprovalID == "" {
		return out.Allowed
	}
	res := e.Approvals.Wait(ctx, d.ApprovalID)
	d.ApprovalStatus = res.Status
	out.Allowed = res.Allowed
	if res.Allowed && res.Status == approvals.StatusApproved && out.Action.Kind == core.KindToolCall {
		e.grant(out.Action)
	}
	return res.Allowed
}

func (e *Engine) DecideAndWait(ctx context.Context, a *core.Action) *Outcome {
	out := e.Decide(ctx, a)
	if out.Decision.Verdict == core.VerdictHold {
		e.Await(ctx, out)
	}
	return out
}

func (e *Engine) Usage(a *core.Action, tokens int, cost float64) {
	if e.Detect.Limiter != nil {
		e.Detect.Limiter.Usage(a.AgentID, tokens, cost)
	}
}

func (o *Outcome) Message() string {
	d := o.Decision
	switch d.Verdict {
	case core.VerdictHold:
		if o.Allowed {
			return "approved by " + d.ApprovalStatus
		}
		return fmt.Sprintf("Rungated held this action for approval (%s) and it was not approved: %s", d.ApprovalID, d.Summary())
	case core.VerdictBlock, core.VerdictContain:
		return fmt.Sprintf("Rungated blocked this action (risk %d, %s): %s", d.Score, d.Verdict, d.Summary())
	}
	return ""
}

var guardedClassPrefixes = []string{"pii.", "pci.", "phi.", "secret.", "financial."}

func egressClassFindings(a *core.Action, m *manifest.Manifest, fs []core.Finding) []core.Finding {
	if a.Kind != core.KindEgressRequest || m == nil || len(m.Spec.DataClasses.Allowed) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []core.Finding
	for _, f := range fs {
		c := f.DataClass
		if c == "" || seen[c] {
			continue
		}
		guarded := false
		for _, p := range guardedClassPrefixes {
			if strings.HasPrefix(c, p) {
				guarded = true
				break
			}
		}
		if !guarded {
			continue
		}
		allowed := false
		for _, pat := range m.Spec.DataClasses.Allowed {
			if manifest.ClassMatch(pat, c) {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		seen[c] = true
		out = append(out, core.Finding{Detector: "policy", Tier: 0, Category: core.CatEgress, Rule: "data_class.not_allowed", Severity: 0.6, Confidence: 1, DataClass: c, Message: c + " is not in the agent's allowed data classes and is leaving to " + a.Host, Min: core.VerdictHold})
	}
	return out
}
