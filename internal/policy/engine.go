package policy

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amareshhebbar/rungated/pkg/manifest"
	"github.com/cedar-policy/cedar-go"
)

type Request struct {
	AgentID     string
	Action      string
	Model       string
	Provider    string
	MaxTokens   int
	Server      string
	Tool        string
	Method      string
	Args        map[string]any
	Host        string
	HTTPMethod  string
	DataClasses []string
}

type Result struct {
	Allowed  bool     `json:"allowed"`
	Reasons  []string `json:"reasons,omitempty"`
	Errors   []string `json:"errors,omitempty"`
	Micros   int64    `json:"micros"`
	Explicit bool     `json:"explicit_forbid,omitempty"`
}

type compiled struct {
	manifest *manifest.Manifest
	source   string
	set      *cedar.PolicySet
	forbids  map[string]bool
	regex    map[string]map[string]*regexp.Regexp
	entities cedar.EntityMap
}

type Engine struct {
	mu     sync.RWMutex
	agents map[string]*compiled
	custom map[string]string
}

func NewEngine() *Engine {
	return &Engine{agents: map[string]*compiled{}, custom: map[string]string{}}
}

func parseInto(set *cedar.PolicySet, forbids map[string]bool, name, src string) error {
	list, err := cedar.NewPolicyListFromBytes(name, []byte(src))
	if err != nil {
		return err
	}
	for i, p := range list {
		id := fmt.Sprintf("%s.%d", name, i)
		if v, ok := p.Annotations()["id"]; ok {
			id = string(v)
		}
		if !set.Add(cedar.PolicyID(id), p) {
			return fmt.Errorf("duplicate policy id %q", id)
		}
		if p.Effect() == cedar.Forbid {
			forbids[id] = true
		}
	}
	return nil
}

func ValidateCedar(src string) error {
	_, err := cedar.NewPolicyListFromBytes("custom", []byte(src))
	return err
}

func (e *Engine) build(m *manifest.Manifest) (*compiled, error) {
	src, err := Compile(m)
	if err != nil {
		return nil, err
	}
	set := cedar.NewPolicySet()
	forbids := map[string]bool{}
	if err := parseInto(set, forbids, "manifest:"+m.Metadata.ID, src); err != nil {
		return nil, fmt.Errorf("compiled policy failed to parse: %w", err)
	}
	if c := e.custom[m.Metadata.Tenant]; c != "" {
		if err := parseInto(set, forbids, "tenant:"+m.Metadata.Tenant, c); err != nil {
			return nil, fmt.Errorf("tenant policy: %w", err)
		}
	}
	if c := e.custom["*"]; c != "" {
		if err := parseInto(set, forbids, "global", c); err != nil {
			return nil, fmt.Errorf("global policy: %w", err)
		}
	}
	rx := map[string]map[string]*regexp.Regexp{}
	for _, t := range m.Spec.Tools {
		for n, r := range t.Args {
			if r.Pattern == "" {
				continue
			}
			key := ToolResourceID(t.Server, t.Name)
			if rx[key] == nil {
				rx[key] = map[string]*regexp.Regexp{}
			}
			rx[key][n] = regexp.MustCompile(r.Pattern)
		}
	}
	agentUID := cedar.NewEntityUID("Agent", cedar.String(m.Metadata.ID))
	tenantUID := cedar.NewEntityUID("Tenant", cedar.String(m.Metadata.Tenant))
	ents := cedar.EntityMap{
		agentUID: cedar.Entity{
			UID:     agentUID,
			Parents: cedar.NewEntityUIDSet(tenantUID),
			Attributes: cedar.NewRecord(cedar.RecordMap{
				"risk_tier": cedar.String(m.Spec.RiskTier),
				"owner":     cedar.String(m.Metadata.Owner),
				"tenant":    cedar.String(m.Metadata.Tenant),
			}),
		},
		tenantUID: cedar.Entity{UID: tenantUID, Parents: cedar.NewEntityUIDSet(), Attributes: cedar.NewRecord(cedar.RecordMap{})},
	}
	return &compiled{manifest: m, source: src, set: set, forbids: forbids, regex: rx, entities: ents}, nil
}

func (e *Engine) Load(m *manifest.Manifest) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, err := e.build(m)
	if err != nil {
		return err
	}
	e.agents[m.Metadata.ID] = c
	return nil
}

func (e *Engine) Remove(agentID string) {
	e.mu.Lock()
	delete(e.agents, agentID)
	e.mu.Unlock()
}

func (e *Engine) SetCustom(tenant, src string) error {
	if err := ValidateCedar(src); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	old := e.custom[tenant]
	e.custom[tenant] = src
	rebuilt := map[string]*compiled{}
	for id, c := range e.agents {
		if tenant != "*" && c.manifest.Metadata.Tenant != tenant {
			continue
		}
		nc, err := e.build(c.manifest)
		if err != nil {
			e.custom[tenant] = old
			return err
		}
		rebuilt[id] = nc
	}
	for id, c := range rebuilt {
		e.agents[id] = c
	}
	return nil
}

func (e *Engine) Custom(tenant string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.custom[tenant]
}

func (e *Engine) Source(agentID string) (string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	c, ok := e.agents[agentID]
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString(c.source)
	if s := e.custom[c.manifest.Metadata.Tenant]; s != "" {
		b.WriteString("\n" + s)
	}
	if s := e.custom["*"]; s != "" {
		b.WriteString("\n" + s)
	}
	return b.String(), true
}

func ToCedar(v any) (cedar.Value, bool) {
	switch x := v.(type) {
	case nil:
		return nil, false
	case string:
		return cedar.String(x), true
	case bool:
		return cedar.Boolean(x), true
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return cedar.Long(i), true
		}
		if f, err := x.Float64(); err == nil {
			return floatValue(f), true
		}
		return cedar.String(x.String()), true
	case float64:
		return floatValue(x), true
	case float32:
		return floatValue(float64(x)), true
	case int:
		return cedar.Long(x), true
	case int64:
		return cedar.Long(x), true
	case int32:
		return cedar.Long(x), true
	case []any:
		vals := make([]cedar.Value, 0, len(x))
		for _, it := range x {
			if cv, ok := ToCedar(it); ok {
				vals = append(vals, cv)
			}
		}
		return cedar.NewSet(vals...), true
	case []string:
		vals := make([]cedar.Value, 0, len(x))
		for _, it := range x {
			vals = append(vals, cedar.String(it))
		}
		return cedar.NewSet(vals...), true
	case map[string]any:
		rm := cedar.RecordMap{}
		for k, it := range x {
			if cv, ok := ToCedar(it); ok {
				rm[cedar.String(k)] = cv
			}
		}
		return cedar.NewRecord(rm), true
	}
	return cedar.String(fmt.Sprint(v)), true
}

func floatValue(f float64) cedar.Value {
	if f == math.Trunc(f) && math.Abs(f) < 9e15 {
		return cedar.Long(int64(f))
	}
	return cedar.String(strconv.FormatFloat(f, 'f', -1, 64))
}

func ArgType(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 9e15 {
			return "integer"
		}
		return "number"
	case float32:
		return ArgType(float64(x))
	case int, int64, int32:
		return "integer"
	case []any, []string:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

func numeric(v any) (float64, bool) {
	switch x := v.(type) {
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	}
	return 0, false
}

func argLen(v any) int {
	switch x := v.(type) {
	case string:
		return utf8.RuneCountInString(x)
	case []any:
		return len(x)
	case []string:
		return len(x)
	case map[string]any:
		return len(x)
	}
	return 0
}

func (e *Engine) Evaluate(req Request) Result {
	start := time.Now()
	e.mu.RLock()
	c, ok := e.agents[req.AgentID]
	e.mu.RUnlock()
	if !ok {
		return Result{Allowed: false, Errors: []string{"unknown agent " + req.AgentID}, Micros: time.Since(start).Microseconds()}
	}
	ctx := cedar.RecordMap{
		"data_classes": stringSet(ExpandClasses(req.DataClasses)),
	}
	var resource cedar.EntityUID
	switch req.Action {
	case ActionLLM:
		resource = cedar.NewEntityUID("Model", cedar.String(req.Model))
		ctx["model"] = cedar.String(req.Model)
		ctx["provider"] = cedar.String(req.Provider)
		ctx["max_tokens"] = cedar.Long(req.MaxTokens)
	case ActionTool:
		resource = cedar.NewEntityUID("Tool", cedar.String(ToolResourceID(req.Server, req.Tool)))
		ctx["tool"] = cedar.String(req.Tool)
		ctx["server"] = cedar.String(req.Server)
		ctx["method"] = cedar.String(req.Method)
		args := cedar.RecordMap{}
		types := cedar.RecordMap{}
		lens := cedar.RecordMap{}
		millis := cedar.RecordMap{}
		regexOK := cedar.RecordMap{}
		names := make([]cedar.Value, 0, len(req.Args))
		for k, v := range req.Args {
			names = append(names, cedar.String(k))
			if cv, ok := ToCedar(v); ok {
				args[cedar.String(k)] = cv
			}
			types[cedar.String(k)] = cedar.String(ArgType(v))
			lens[cedar.String(k)] = cedar.Long(argLen(v))
			if f, ok := numeric(v); ok {
				millis[cedar.String(k)] = cedar.Long(milli(f))
			}
		}
		for _, t := range c.manifest.Spec.Tools {
			if t.Name != req.Tool || (t.Server != "" && t.Server != req.Server) {
				continue
			}
			key := ToolResourceID(t.Server, t.Name)
			for n, re := range c.regex[key] {
				if v, ok := req.Args[n]; ok {
					s, isStr := v.(string)
					regexOK[cedar.String(key+":"+n)] = cedar.Boolean(isStr && re.MatchString(s))
				}
			}
		}
		ctx["args"] = cedar.NewRecord(args)
		ctx["arg_types"] = cedar.NewRecord(types)
		ctx["arg_len"] = cedar.NewRecord(lens)
		ctx["arg_milli"] = cedar.NewRecord(millis)
		ctx["arg_regex"] = cedar.NewRecord(regexOK)
		ctx["arg_names"] = cedar.NewSet(names...)
	case ActionEgress:
		host := strings.ToLower(strings.TrimSuffix(req.Host, "."))
		resource = cedar.NewEntityUID("Host", cedar.String(host))
		ctx["host"] = cedar.String(host)
		ctx["http_method"] = cedar.String(strings.ToUpper(req.HTTPMethod))
	default:
		return Result{Allowed: true, Micros: time.Since(start).Microseconds()}
	}
	creq := cedar.Request{
		Principal: cedar.NewEntityUID("Agent", cedar.String(req.AgentID)),
		Action:    cedar.NewEntityUID("Action", cedar.String(req.Action)),
		Resource:  resource,
		Context:   cedar.NewRecord(ctx),
	}
	dec, diag := c.set.IsAuthorized(c.entities, creq)
	res := Result{Allowed: dec == cedar.Allow}
	for _, r := range diag.Reasons {
		id := string(r.PolicyID)
		res.Reasons = append(res.Reasons, id)
		if c.forbids[id] {
			res.Explicit = true
		}
	}
	for _, er := range diag.Errors {
		res.Errors = append(res.Errors, er.String())
	}
	sort.Strings(res.Reasons)
	res.Micros = time.Since(start).Microseconds()
	return res
}

func stringSet(vals []string) cedar.Set {
	vs := make([]cedar.Value, len(vals))
	for i, v := range vals {
		vs[i] = cedar.String(v)
	}
	return cedar.NewSet(vs...)
}
