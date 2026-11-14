package core

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Channel string

const (
	ChannelLLM    Channel = "llm"
	ChannelMCP    Channel = "mcp"
	ChannelEgress Channel = "egress"
	ChannelSDK    Channel = "sdk"
)

type Kind string

const (
	KindLLMRequest     Kind = "llm.request"
	KindLLMResponse    Kind = "llm.response"
	KindToolCall       Kind = "tool.call"
	KindToolResult     Kind = "tool.result"
	KindMCPToolsList   Kind = "mcp.tools_list"
	KindMCPResource    Kind = "mcp.resource"
	KindMCPPrompt      Kind = "mcp.prompt"
	KindEgressRequest  Kind = "egress.request"
	KindEgressResponse Kind = "egress.response"
)

type Source string

const (
	SourceSystem     Source = "system"
	SourceUser       Source = "user"
	SourceAssistant  Source = "assistant"
	SourceToolResult Source = "tool_result"
	SourceToolArgs   Source = "tool_args"
	SourceWeb        Source = "web"
	SourceFile       Source = "file"
	SourceRAG        Source = "rag"
	SourceToolDesc   Source = "tool_description"
	SourceEgressBody Source = "egress_body"
)

func (s Source) Untrusted() bool {
	switch s {
	case SourceToolResult, SourceWeb, SourceFile, SourceRAG, SourceToolDesc, SourceUser:
		return true
	}
	return false
}

func (s Source) Outbound() bool {
	switch s {
	case SourceUser, SourceSystem, SourceToolResult, SourceToolArgs, SourceEgressBody, SourceAssistant, SourceFile, SourceRAG, SourceWeb:
		return true
	}
	return false
}

type TextPart struct {
	Source Source `json:"source"`
	Text   string `json:"text"`
	Path   string `json:"path,omitempty"`
}

type Action struct {
	ID          string            `json:"id"`
	TenantID    string            `json:"tenant_id"`
	AgentID     string            `json:"agent_id"`
	SessionID   string            `json:"session_id"`
	Channel     Channel           `json:"channel"`
	Kind        Kind              `json:"kind"`
	Provider    string            `json:"provider,omitempty"`
	Model       string            `json:"model,omitempty"`
	Server      string            `json:"server,omitempty"`
	Tool        string            `json:"tool,omitempty"`
	Method      string            `json:"method,omitempty"`
	Args        map[string]any    `json:"args,omitempty"`
	Host        string            `json:"host,omitempty"`
	URL         string            `json:"url,omitempty"`
	Texts       []TextPart        `json:"texts,omitempty"`
	Bytes       int               `json:"bytes,omitempty"`
	Tokens      int               `json:"tokens,omitempty"`
	CostUSD     float64           `json:"cost_usd,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Time        time.Time         `json:"time"`
	Precomputed []Finding         `json:"-"`
}

type Category string

const (
	CatPolicy    Category = "policy"
	CatInjection Category = "injection"
	CatSecret    Category = "secret"
	CatPII       Category = "pii"
	CatPHI       Category = "phi"
	CatEgress    Category = "egress"
	CatRate      Category = "rate"
	CatLoop      Category = "loop"
	CatCost      Category = "cost"
	CatPin       Category = "tool_pin"
	CatExfil     Category = "exfiltration"
	CatJudge     Category = "judge"
	CatContain   Category = "containment"
	CatScope     Category = "scope"
)

type Span struct {
	Part  int `json:"part"`
	Start int `json:"start"`
	End   int `json:"end"`
}

type Finding struct {
	Detector   string   `json:"detector"`
	Tier       int      `json:"tier"`
	Category   Category `json:"category"`
	Rule       string   `json:"rule,omitempty"`
	Severity   float64  `json:"severity"`
	Confidence float64  `json:"confidence"`
	Message    string   `json:"message"`
	DataClass  string   `json:"data_class,omitempty"`
	Span       *Span    `json:"span,omitempty"`
	Redactable bool     `json:"redactable,omitempty"`
	Min        Verdict  `json:"min_verdict,omitempty"`
	Match      string   `json:"-"`
}

type Verdict string

const (
	VerdictAllow   Verdict = "allow"
	VerdictWarn    Verdict = "warn"
	VerdictRedact  Verdict = "redact"
	VerdictHold    Verdict = "hold"
	VerdictBlock   Verdict = "block"
	VerdictContain Verdict = "contain"
)

func (v Verdict) Rank() int {
	switch v {
	case VerdictAllow:
		return 0
	case VerdictWarn:
		return 1
	case VerdictRedact:
		return 2
	case VerdictHold:
		return 3
	case VerdictBlock:
		return 4
	case VerdictContain:
		return 5
	}
	return 0
}

func ValidVerdict(v Verdict) bool {
	switch v {
	case VerdictAllow, VerdictWarn, VerdictRedact, VerdictHold, VerdictBlock, VerdictContain:
		return true
	}
	return false
}

func MaxVerdict(a, b Verdict) Verdict {
	if b.Rank() > a.Rank() {
		return b
	}
	return a
}

type Mode string

const (
	ModeObserve     Mode = "observe"
	ModeRecommend   Mode = "recommend"
	ModeHuman       Mode = "human"
	ModeAutoContain Mode = "auto_contain"
	ModeFullAuto    Mode = "full_auto"
)

func ValidMode(m Mode) bool {
	switch m {
	case ModeObserve, ModeRecommend, ModeHuman, ModeAutoContain, ModeFullAuto:
		return true
	}
	return false
}

type Redaction struct {
	Part  int    `json:"part"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Token string `json:"token"`
	Class string `json:"class"`
}

type Decision struct {
	ActionID       string      `json:"action_id"`
	Score          int         `json:"score"`
	Computed       Verdict     `json:"computed"`
	Verdict        Verdict     `json:"verdict"`
	Mode           Mode        `json:"mode"`
	Findings       []Finding   `json:"findings"`
	PolicyAllowed  bool        `json:"policy_allowed"`
	PolicyReasons  []string    `json:"policy_reasons,omitempty"`
	Reasons        []string    `json:"reasons,omitempty"`
	ApprovalID     string      `json:"approval_id,omitempty"`
	ApprovalStatus string      `json:"approval_status,omitempty"`
	Redactions     []Redaction `json:"redactions,omitempty"`
	RedactedTexts  []string    `json:"redacted_texts,omitempty"`
	LatencyMicros  int64       `json:"latency_us"`
	FailMode       string      `json:"fail_mode,omitempty"`
	Degraded       bool        `json:"degraded,omitempty"`
	JudgeReason    string      `json:"judge_reason,omitempty"`
	IncidentID     string      `json:"incident_id,omitempty"`
}

func (d *Decision) Blocks() bool {
	return d.Verdict == VerdictBlock || d.Verdict == VerdictContain
}

func (d *Decision) Summary() string {
	if len(d.Reasons) > 0 {
		return d.Reasons[0]
	}
	return string(d.Verdict)
}

type Event struct {
	ID        string         `json:"id"`
	Seq       uint64         `json:"seq"`
	Time      time.Time      `json:"time"`
	TenantID  string         `json:"tenant_id"`
	AgentID   string         `json:"agent_id"`
	SessionID string         `json:"session_id"`
	Type      string         `json:"type"`
	Action    *Action        `json:"action,omitempty"`
	Decision  *Decision      `json:"decision,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

func NewID(prefix string) string {
	b := make([]byte, 12)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
