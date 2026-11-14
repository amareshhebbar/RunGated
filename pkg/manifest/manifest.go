package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const APIVersion = "rungated.dev/v0"
const Kind = "Agent"

type Manifest struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       Spec     `yaml:"spec" json:"spec"`
}

type Metadata struct {
	ID      string            `yaml:"id" json:"id"`
	Name    string            `yaml:"name,omitempty" json:"name,omitempty"`
	Owner   string            `yaml:"owner" json:"owner"`
	Purpose string            `yaml:"purpose" json:"purpose"`
	Tenant  string            `yaml:"tenant,omitempty" json:"tenant,omitempty"`
	Version string            `yaml:"version,omitempty" json:"version,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

type Spec struct {
	Model       Model       `yaml:"model" json:"model"`
	RiskTier    string      `yaml:"riskTier" json:"riskTier"`
	Autonomy    Autonomy    `yaml:"autonomy" json:"autonomy"`
	DataClasses DataClasses `yaml:"dataClasses,omitempty" json:"dataClasses,omitempty"`
	Tools       []Tool      `yaml:"tools,omitempty" json:"tools,omitempty"`
	MCPServers  []MCPServer `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Egress      Egress      `yaml:"egress,omitempty" json:"egress,omitempty"`
	Limits      Limits      `yaml:"limits,omitempty" json:"limits,omitempty"`
	Risk        Risk        `yaml:"risk,omitempty" json:"risk,omitempty"`
	Escalation  Escalation  `yaml:"escalation,omitempty" json:"escalation,omitempty"`
	Privacy     Privacy     `yaml:"privacy,omitempty" json:"privacy,omitempty"`
}

type Model struct {
	Providers           []string `yaml:"providers,omitempty" json:"providers,omitempty"`
	Allowed             []string `yaml:"allowed" json:"allowed"`
	MaxTokensPerRequest int      `yaml:"maxTokensPerRequest,omitempty" json:"maxTokensPerRequest,omitempty"`
}

type Autonomy struct {
	Default   string             `yaml:"default" json:"default"`
	Overrides []AutonomyOverride `yaml:"overrides,omitempty" json:"overrides,omitempty"`
}

type AutonomyOverride struct {
	ActionClass string `yaml:"actionClass,omitempty" json:"actionClass,omitempty"`
	Tool        string `yaml:"tool,omitempty" json:"tool,omitempty"`
	Mode        string `yaml:"mode" json:"mode"`
}

type DataClasses struct {
	Allowed     []string `yaml:"allowed,omitempty" json:"allowed,omitempty"`
	NeverEgress []string `yaml:"neverEgress,omitempty" json:"neverEgress,omitempty"`
	NeverModel  []string `yaml:"neverModel,omitempty" json:"neverModel,omitempty"`
}

type ArgRule struct {
	Type      string   `yaml:"type,omitempty" json:"type,omitempty"`
	Required  bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Enum      []string `yaml:"enum,omitempty" json:"enum,omitempty"`
	Like      []string `yaml:"like,omitempty" json:"like,omitempty"`
	Pattern   string   `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Min       *float64 `yaml:"min,omitempty" json:"min,omitempty"`
	Max       *float64 `yaml:"max,omitempty" json:"max,omitempty"`
	MaxLength int      `yaml:"maxLength,omitempty" json:"maxLength,omitempty"`
	Deny      []string `yaml:"deny,omitempty" json:"deny,omitempty"`
}

type Tool struct {
	Name            string             `yaml:"name" json:"name"`
	Server          string             `yaml:"server,omitempty" json:"server,omitempty"`
	Description     string             `yaml:"description,omitempty" json:"description,omitempty"`
	Methods         []string           `yaml:"methods,omitempty" json:"methods,omitempty"`
	Args            map[string]ArgRule `yaml:"args,omitempty" json:"args,omitempty"`
	ExtraArgs       bool               `yaml:"allowExtraArgs,omitempty" json:"allowExtraArgs,omitempty"`
	BlastRadius     string             `yaml:"blastRadius,omitempty" json:"blastRadius,omitempty"`
	DataClasses     []string           `yaml:"dataClasses,omitempty" json:"dataClasses,omitempty"`
	RequireApproval bool               `yaml:"requireApproval,omitempty" json:"requireApproval,omitempty"`
}

type MCPServer struct {
	Name    string            `yaml:"name" json:"name"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Command []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Pinning string            `yaml:"pinning,omitempty" json:"pinning,omitempty"`
}

type Egress struct {
	Allow      []string `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny       []string `yaml:"deny,omitempty" json:"deny,omitempty"`
	Methods    []string `yaml:"methods,omitempty" json:"methods,omitempty"`
	ScanBodies *bool    `yaml:"scanBodies,omitempty" json:"scanBodies,omitempty"`
	MaxBodyKB  int      `yaml:"maxBodyKB,omitempty" json:"maxBodyKB,omitempty"`
}

type Limits struct {
	RequestsPerMinute  int     `yaml:"requestsPerMinute,omitempty" json:"requestsPerMinute,omitempty"`
	ToolCallsPerMinute int     `yaml:"toolCallsPerMinute,omitempty" json:"toolCallsPerMinute,omitempty"`
	TokensPerHour      int     `yaml:"tokensPerHour,omitempty" json:"tokensPerHour,omitempty"`
	CostPerDayUSD      float64 `yaml:"costPerDayUSD,omitempty" json:"costPerDayUSD,omitempty"`
	MaxLoopRepeats     int     `yaml:"maxLoopRepeats,omitempty" json:"maxLoopRepeats,omitempty"`
	EgressBytesPerHour int64   `yaml:"egressBytesPerHour,omitempty" json:"egressBytesPerHour,omitempty"`
}

type Thresholds struct {
	Warn    int `yaml:"warn,omitempty" json:"warn,omitempty"`
	Redact  int `yaml:"redact,omitempty" json:"redact,omitempty"`
	Hold    int `yaml:"hold,omitempty" json:"hold,omitempty"`
	Block   int `yaml:"block,omitempty" json:"block,omitempty"`
	Contain int `yaml:"contain,omitempty" json:"contain,omitempty"`
}

type Risk struct {
	Thresholds      Thresholds         `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
	FailMode        string             `yaml:"failMode,omitempty" json:"failMode,omitempty"`
	Weights         map[string]float64 `yaml:"weights,omitempty" json:"weights,omitempty"`
	JudgeOnHold     *bool              `yaml:"judgeOnHold,omitempty" json:"judgeOnHold,omitempty"`
	JudgeCanRelease bool               `yaml:"judgeCanRelease,omitempty" json:"judgeCanRelease,omitempty"`
}

type Escalation struct {
	Approvers      []string `yaml:"approvers,omitempty" json:"approvers,omitempty"`
	SlackChannel   string   `yaml:"slackChannel,omitempty" json:"slackChannel,omitempty"`
	TimeoutSeconds int      `yaml:"timeoutSeconds,omitempty" json:"timeoutSeconds,omitempty"`
	OnTimeout      string   `yaml:"onTimeout,omitempty" json:"onTimeout,omitempty"`
}

type Privacy struct {
	RedactBeforeModel *bool    `yaml:"redactBeforeModel,omitempty" json:"redactBeforeModel,omitempty"`
	RestoreInToolArgs *bool    `yaml:"restoreInToolArgs,omitempty" json:"restoreInToolArgs,omitempty"`
	RedactClasses     []string `yaml:"redactClasses,omitempty" json:"redactClasses,omitempty"`
	StoreContent      *bool    `yaml:"storeContent,omitempty" json:"storeContent,omitempty"`
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
var toolNameRe = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,128}$`)

var RiskTiers = []string{"low", "medium", "high", "critical"}
var Modes = []string{"observe", "recommend", "human", "auto_contain", "full_auto"}
var BlastRadii = []string{"low", "medium", "high"}
var ArgTypes = []string{"string", "integer", "number", "boolean", "array", "object", "any"}
var ActionClasses = []string{"llm.request", "llm.response", "tool.call", "tool.result", "mcp.tools_list", "mcp.resource", "mcp.prompt", "egress.request", "egress.response", "*"}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Field + ": " + e.Message }

type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (v *ValidationError) Error() string {
	parts := make([]string, len(v.Errors))
	for i, e := range v.Errors {
		parts[i] = e.Error()
	}
	return "invalid manifest: " + strings.Join(parts, "; ")
}

func Parse(data []byte) (*Manifest, error) {
	m := &Manifest{}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.DisallowUnknownFields()
		if err := dec.Decode(m); err != nil {
			return nil, fmt.Errorf("parse manifest json: %w", err)
		}
	} else {
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(m); err != nil {
			return nil, fmt.Errorf("parse manifest yaml: %w", err)
		}
	}
	m.ApplyDefaults()
	if err := m.Validate(); err != nil {
		return m, err
	}
	return m, nil
}

func Load(file string) (*Manifest, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func boolPtr(b bool) *bool { return &b }

func (m *Manifest) ApplyDefaults() {
	if m.APIVersion == "" {
		m.APIVersion = APIVersion
	}
	if m.Kind == "" {
		m.Kind = Kind
	}
	if m.Metadata.Tenant == "" {
		m.Metadata.Tenant = "default"
	}
	if m.Spec.RiskTier == "" {
		m.Spec.RiskTier = "medium"
	}
	if m.Spec.Autonomy.Default == "" {
		m.Spec.Autonomy.Default = "observe"
	}
	t := &m.Spec.Risk.Thresholds
	if t.Warn == 0 {
		t.Warn = 20
	}
	if t.Redact == 0 {
		t.Redact = 30
	}
	if t.Hold == 0 {
		t.Hold = 60
	}
	if t.Block == 0 {
		t.Block = 80
	}
	if t.Contain == 0 {
		t.Contain = 95
	}
	if m.Spec.Risk.FailMode == "" {
		m.Spec.Risk.FailMode = "closed"
	}
	if m.Spec.Risk.JudgeOnHold == nil {
		m.Spec.Risk.JudgeOnHold = boolPtr(true)
	}
	if m.Spec.Escalation.TimeoutSeconds == 0 {
		m.Spec.Escalation.TimeoutSeconds = 300
	}
	if m.Spec.Escalation.OnTimeout == "" {
		m.Spec.Escalation.OnTimeout = "deny"
	}
	if m.Spec.Egress.ScanBodies == nil {
		m.Spec.Egress.ScanBodies = boolPtr(true)
	}
	if m.Spec.Egress.MaxBodyKB == 0 {
		m.Spec.Egress.MaxBodyKB = 1024
	}
	if m.Spec.Limits.MaxLoopRepeats == 0 {
		m.Spec.Limits.MaxLoopRepeats = 6
	}
	if m.Spec.Privacy.RedactBeforeModel == nil {
		m.Spec.Privacy.RedactBeforeModel = boolPtr(true)
	}
	if m.Spec.Privacy.RestoreInToolArgs == nil {
		m.Spec.Privacy.RestoreInToolArgs = boolPtr(true)
	}
	if m.Spec.Privacy.StoreContent == nil {
		m.Spec.Privacy.StoreContent = boolPtr(true)
	}
	if len(m.Spec.Privacy.RedactClasses) == 0 {
		m.Spec.Privacy.RedactClasses = []string{"pii.*", "secret.*", "pci.*", "phi.*"}
	}
	for i := range m.Spec.Tools {
		if m.Spec.Tools[i].BlastRadius == "" {
			m.Spec.Tools[i].BlastRadius = "medium"
		}
	}
	for i := range m.Spec.MCPServers {
		if m.Spec.MCPServers[i].Pinning == "" {
			m.Spec.MCPServers[i].Pinning = "tofu"
		}
	}
}

func in(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (m *Manifest) Validate() error {
	var errs []FieldError
	add := func(f, msg string, a ...any) { errs = append(errs, FieldError{f, fmt.Sprintf(msg, a...)}) }
	if m.APIVersion != APIVersion {
		add("apiVersion", "must be %q", APIVersion)
	}
	if m.Kind != Kind {
		add("kind", "must be %q", Kind)
	}
	if !idRe.MatchString(m.Metadata.ID) {
		add("metadata.id", "must match %s", idRe.String())
	}
	if !idRe.MatchString(m.Metadata.Tenant) {
		add("metadata.tenant", "must match %s", idRe.String())
	}
	if strings.TrimSpace(m.Metadata.Owner) == "" {
		add("metadata.owner", "is required")
	}
	if strings.TrimSpace(m.Metadata.Purpose) == "" {
		add("metadata.purpose", "is required")
	}
	if len(m.Spec.Model.Allowed) == 0 {
		add("spec.model.allowed", "must list at least one model pattern")
	}
	for i, p := range m.Spec.Model.Allowed {
		if err := checkGlob(p); err != nil {
			add(fmt.Sprintf("spec.model.allowed[%d]", i), "%v", err)
		}
	}
	if !in(RiskTiers, m.Spec.RiskTier) {
		add("spec.riskTier", "must be one of %v", RiskTiers)
	}
	if !in(Modes, m.Spec.Autonomy.Default) {
		add("spec.autonomy.default", "must be one of %v", Modes)
	}
	for i, o := range m.Spec.Autonomy.Overrides {
		f := fmt.Sprintf("spec.autonomy.overrides[%d]", i)
		if !in(Modes, o.Mode) {
			add(f+".mode", "must be one of %v", Modes)
		}
		if o.ActionClass != "" && !in(ActionClasses, o.ActionClass) {
			add(f+".actionClass", "must be one of %v", ActionClasses)
		}
		if o.ActionClass == "" && o.Tool == "" {
			add(f, "needs actionClass or tool")
		}
	}
	seen := map[string]bool{}
	servers := map[string]bool{}
	for i, s := range m.Spec.MCPServers {
		f := fmt.Sprintf("spec.mcpServers[%d]", i)
		if !idRe.MatchString(s.Name) {
			add(f+".name", "must match %s", idRe.String())
		}
		if servers[s.Name] {
			add(f+".name", "duplicate server %q", s.Name)
		}
		servers[s.Name] = true
		if s.URL == "" && len(s.Command) == 0 {
			add(f, "needs url or command")
		}
		if !in([]string{"tofu", "strict", "off"}, s.Pinning) {
			add(f+".pinning", "must be tofu, strict or off")
		}
	}
	for i, t := range m.Spec.Tools {
		f := fmt.Sprintf("spec.tools[%d]", i)
		if !toolNameRe.MatchString(t.Name) {
			add(f+".name", "invalid tool name")
		}
		key := t.Server + "/" + t.Name
		if seen[key] {
			add(f+".name", "duplicate tool %q", key)
		}
		seen[key] = true
		if t.Server != "" && len(m.Spec.MCPServers) > 0 && !servers[t.Server] {
			add(f+".server", "unknown server %q", t.Server)
		}
		if !in(BlastRadii, t.BlastRadius) {
			add(f+".blastRadius", "must be one of %v", BlastRadii)
		}
		names := make([]string, 0, len(t.Args))
		for n := range t.Args {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			r := t.Args[n]
			af := f + ".args." + n
			if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`).MatchString(n) {
				add(af, "invalid argument name")
			}
			if r.Type != "" && !in(ArgTypes, r.Type) {
				add(af+".type", "must be one of %v", ArgTypes)
			}
			if r.Pattern != "" {
				if _, err := regexp.Compile(r.Pattern); err != nil {
					add(af+".pattern", "bad regex: %v", err)
				}
			}
			for j, l := range append(append([]string{}, r.Like...), r.Deny...) {
				if err := checkGlob(l); err != nil {
					add(fmt.Sprintf("%s.like[%d]", af, j), "%v", err)
				}
			}
			if r.Min != nil && r.Max != nil && *r.Min > *r.Max {
				add(af, "min greater than max")
			}
			if (r.Min != nil || r.Max != nil) && r.Type != "" && r.Type != "integer" && r.Type != "number" {
				add(af, "min and max need type integer or number")
			}
		}
	}
	for i, d := range append(append([]string{}, m.Spec.Egress.Allow...), m.Spec.Egress.Deny...) {
		if err := checkHostPattern(d); err != nil {
			add(fmt.Sprintf("spec.egress[%d]", i), "%v", err)
		}
	}
	th := m.Spec.Risk.Thresholds
	if !(th.Warn <= th.Redact && th.Redact <= th.Hold && th.Hold <= th.Block && th.Block <= th.Contain && th.Contain <= 100 && th.Warn >= 0) {
		add("spec.risk.thresholds", "must satisfy 0 <= warn <= redact <= hold <= block <= contain <= 100")
	}
	if !in([]string{"open", "closed"}, m.Spec.Risk.FailMode) {
		add("spec.risk.failMode", "must be open or closed")
	}
	if !in([]string{"deny", "allow"}, m.Spec.Escalation.OnTimeout) {
		add("spec.escalation.onTimeout", "must be deny or allow")
	}
	if m.Spec.Escalation.TimeoutSeconds < 1 || m.Spec.Escalation.TimeoutSeconds > 86400 {
		add("spec.escalation.timeoutSeconds", "must be between 1 and 86400")
	}
	if m.Spec.Limits.RequestsPerMinute < 0 || m.Spec.Limits.ToolCallsPerMinute < 0 || m.Spec.Limits.TokensPerHour < 0 || m.Spec.Limits.CostPerDayUSD < 0 {
		add("spec.limits", "must not be negative")
	}
	for i, c := range append(append(append([]string{}, m.Spec.DataClasses.Allowed...), m.Spec.DataClasses.NeverEgress...), m.Spec.DataClasses.NeverModel...) {
		if !regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_*]+)*$|^\*$`).MatchString(c) {
			add(fmt.Sprintf("spec.dataClasses[%d]", i), "invalid data class %q", c)
		}
	}
	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

func checkGlob(p string) error {
	if p == "" {
		return errors.New("empty pattern")
	}
	if strings.ContainsAny(p, "\"\\\n") {
		return errors.New("pattern contains forbidden characters")
	}
	return nil
}

func checkHostPattern(p string) error {
	if p == "" {
		return errors.New("empty host pattern")
	}
	if strings.ContainsAny(p, "/\"\\ ") {
		return fmt.Errorf("host pattern %q must be a bare host or wildcard like *.example.com", p)
	}
	return nil
}

func (m *Manifest) FindTool(server, name string) *Tool {
	var fallback *Tool
	for i := range m.Spec.Tools {
		t := &m.Spec.Tools[i]
		if t.Name != name {
			continue
		}
		if t.Server == server {
			return t
		}
		if t.Server == "" && fallback == nil {
			fallback = t
		}
	}
	if fallback == nil && server == "" {
		for i := range m.Spec.Tools {
			if m.Spec.Tools[i].Name == name {
				return &m.Spec.Tools[i]
			}
		}
	}
	return fallback
}

func (m *Manifest) FindServer(name string) *MCPServer {
	for i := range m.Spec.MCPServers {
		if m.Spec.MCPServers[i].Name == name {
			return &m.Spec.MCPServers[i]
		}
	}
	return nil
}

func (m *Manifest) ModeFor(actionClass, tool string) string {
	mode := m.Spec.Autonomy.Default
	for _, o := range m.Spec.Autonomy.Overrides {
		if o.Tool != "" && o.Tool == tool && (o.ActionClass == "" || o.ActionClass == actionClass || o.ActionClass == "*") {
			return o.Mode
		}
	}
	for _, o := range m.Spec.Autonomy.Overrides {
		if o.Tool == "" && (o.ActionClass == actionClass || o.ActionClass == "*") {
			mode = o.Mode
		}
	}
	return mode
}

func Glob(pattern, s string) bool {
	return globMatch(pattern, s)
}

func globMatch(p, s string) bool {
	for len(p) > 0 {
		if p[0] == '*' {
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if globMatch(p, s[i:]) {
					return true
				}
			}
			return false
		}
		if s == "" || p[0] != s[0] {
			return false
		}
		p, s = p[1:], s[1:]
	}
	return s == ""
}

func HostMatch(pattern, host string) bool {
	pattern = strings.ToLower(pattern)
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if strings.HasPrefix(pattern, "*.") {
		return host == pattern[2:] || strings.HasSuffix(host, pattern[1:])
	}
	return globMatch(pattern, host)
}

func ClassMatch(pattern, class string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		base := strings.TrimSuffix(pattern, ".*")
		return class == base || strings.HasPrefix(class, base+".")
	}
	return pattern == class || strings.HasPrefix(class, pattern+".")
}

func (m *Manifest) YAML() ([]byte, error) {
	return yaml.Marshal(m)
}
