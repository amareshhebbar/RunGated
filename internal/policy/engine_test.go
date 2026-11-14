package policy

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/amareshhebbar/rungated/pkg/manifest"
)

func loadExample(t testing.TB) (*Engine, *manifest.Manifest) {
	m, err := manifest.Load("../../spec/ats/examples/finance-agent.yaml")
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	if err := e.Load(m); err != nil {
		src, _ := Compile(m)
		t.Fatalf("%v\n%s", err, src)
	}
	return e, m
}

func TestExamplePolicy(t *testing.T) {
	e, _ := loadExample(t)
	cases := []struct {
		name string
		req  Request
		want bool
	}{
		{"model ok", Request{Action: ActionLLM, Model: "gpt-4o-mini", Provider: "openai", MaxTokens: 1000}, true},
		{"model denied", Request{Action: ActionLLM, Model: "llama-3", Provider: "openai"}, false},
		{"provider denied", Request{Action: ActionLLM, Model: "gpt-4o", Provider: "groq"}, false},
		{"too many tokens", Request{Action: ActionLLM, Model: "gpt-4o", Provider: "openai", MaxTokens: 99999}, false},
		{"secret to model", Request{Action: ActionLLM, Model: "gpt-4o", Provider: "openai", DataClasses: []string{"secret.aws.access_key"}}, false},
		{"invoice ok", Request{Action: ActionTool, Server: "finance-mcp", Tool: "get_invoice", Method: "read", Args: map[string]any{"invoice_id": "INV-000123"}}, true},
		{"invoice bad id", Request{Action: ActionTool, Server: "finance-mcp", Tool: "get_invoice", Args: map[string]any{"invoice_id": "INV-1 OR 1=1"}}, false},
		{"invoice missing", Request{Action: ActionTool, Server: "finance-mcp", Tool: "get_invoice", Args: map[string]any{}}, false},
		{"invoice wrong method", Request{Action: ActionTool, Server: "finance-mcp", Tool: "get_invoice", Method: "delete", Args: map[string]any{"invoice_id": "INV-000123"}}, false},
		{"extra arg", Request{Action: ActionTool, Server: "finance-mcp", Tool: "get_invoice", Args: map[string]any{"invoice_id": "INV-000123", "x": 1}}, false},
		{"list ok", Request{Action: ActionTool, Server: "finance-mcp", Tool: "list_customers", Args: map[string]any{"limit": json.Number("10"), "region": "us"}}, true},
		{"list limit high", Request{Action: ActionTool, Server: "finance-mcp", Tool: "list_customers", Args: map[string]any{"limit": 500.0}}, false},
		{"list limit frac", Request{Action: ActionTool, Server: "finance-mcp", Tool: "list_customers", Args: map[string]any{"limit": 50.5}}, false},
		{"list region bad", Request{Action: ActionTool, Server: "finance-mcp", Tool: "list_customers", Args: map[string]any{"region": "cn"}}, false},
		{"email internal", Request{Action: ActionTool, Server: "finance-mcp", Tool: "send_email", Args: map[string]any{"to": "cfo@acme.example", "subject": "hi"}}, true},
		{"email external", Request{Action: ActionTool, Server: "finance-mcp", Tool: "send_email", Args: map[string]any{"to": "attacker@evil.example"}}, false},
		{"email with ssn", Request{Action: ActionTool, Server: "finance-mcp", Tool: "send_email", Args: map[string]any{"to": "cfo@acme.example"}, DataClasses: []string{"pii.ssn"}}, false},
		{"email wrong server", Request{Action: ActionTool, Server: "other", Tool: "send_email", Args: map[string]any{"to": "cfo@acme.example"}}, false},
		{"unknown tool", Request{Action: ActionTool, Server: "finance-mcp", Tool: "drop_tables", Args: map[string]any{}}, false},
		{"fetch ok any server", Request{Action: ActionTool, Server: "", Tool: "fetch_url", Args: map[string]any{"url": "https://docs.python.org/3/"}}, true},
		{"fetch ssrf", Request{Action: ActionTool, Tool: "fetch_url", Args: map[string]any{"url": "http://169.254.169.254/latest/meta-data"}}, false},
		{"fetch file scheme", Request{Action: ActionTool, Tool: "fetch_url", Args: map[string]any{"url": "file:///etc/passwd"}}, false},
		{"egress ok", Request{Action: ActionEgress, Host: "api.stripe.com", HTTPMethod: "post"}, true},
		{"egress apex", Request{Action: ActionEgress, Host: "stripe.com", HTTPMethod: "GET"}, true},
		{"egress lookalike", Request{Action: ActionEgress, Host: "stripe.com.evil.io", HTTPMethod: "GET"}, false},
		{"egress unknown", Request{Action: ActionEgress, Host: "example.org", HTTPMethod: "GET"}, false},
		{"egress method", Request{Action: ActionEgress, Host: "api.acme.example", HTTPMethod: "DELETE"}, false},
		{"egress card", Request{Action: ActionEgress, Host: "api.acme.example", HTTPMethod: "POST", DataClasses: []string{"pci.card"}}, false},
	}
	for _, c := range cases {
		c.req.AgentID = "finance-agent"
		r := e.Evaluate(c.req)
		if r.Allowed != c.want {
			t.Errorf("%s: got %v want %v reasons=%v errors=%v", c.name, r.Allowed, c.want, r.Reasons, r.Errors)
		}
	}
}

func TestCustomTenantPolicy(t *testing.T) {
	e, _ := loadExample(t)
	err := e.SetCustom("acme", `@id("no-big-lists") forbid (principal in Tenant::"acme", action == Action::"tool.call", resource) when { context.tool == "list_customers" && principal.risk_tier == "high" };`)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Evaluate(Request{AgentID: "finance-agent", Action: ActionTool, Server: "finance-mcp", Tool: "list_customers", Args: map[string]any{"limit": 5.0}})
	if r.Allowed || !r.Explicit {
		t.Fatalf("custom forbid not applied: %+v", r)
	}
	if err := e.SetCustom("acme", "permit (principal"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestUnknownAgentDenied(t *testing.T) {
	e := NewEngine()
	if e.Evaluate(Request{AgentID: "nope", Action: ActionLLM, Model: "x"}).Allowed {
		t.Fatal("unknown agent allowed")
	}
}

func TestCedarQuoteInjection(t *testing.T) {
	m := &manifest.Manifest{Metadata: manifest.Metadata{ID: "a1", Owner: "o", Purpose: "p"}, Spec: manifest.Spec{Model: manifest.Model{Allowed: []string{"m*"}}}}
	m.ApplyDefaults()
	m.Spec.Tools = []manifest.Tool{{Name: "t", BlastRadius: "low", Args: map[string]manifest.ArgRule{"a": {Enum: []string{`x"} ; permit(principal, action, resource);//`}}}}}
	e := NewEngine()
	if err := e.Load(m); err != nil {
		t.Fatal(err)
	}
	r := e.Evaluate(Request{AgentID: "a1", Action: ActionTool, Tool: "other"})
	if r.Allowed {
		t.Fatal("quote injection escalated privileges")
	}
}

func reference(m *manifest.Manifest, req Request) bool {
	classes := map[string]bool{}
	for _, c := range ExpandClasses(req.DataClasses) {
		classes[c] = true
	}
	anyClass := func(list []string) bool {
		for _, l := range list {
			if classes[l] {
				return true
			}
		}
		return false
	}
	switch req.Action {
	case ActionLLM:
		ok := false
		for _, p := range m.Spec.Model.Allowed {
			if manifest.Glob(p, req.Model) {
				ok = true
			}
		}
		if len(m.Spec.Model.Providers) > 0 {
			found := false
			for _, p := range m.Spec.Model.Providers {
				if p == req.Provider {
					found = true
				}
			}
			ok = ok && found
		}
		if m.Spec.Model.MaxTokensPerRequest > 0 && req.MaxTokens > m.Spec.Model.MaxTokensPerRequest {
			ok = false
		}
		return ok && !anyClass(m.Spec.DataClasses.NeverModel)
	case ActionTool:
		if anyClass(m.Spec.DataClasses.NeverEgress) {
			return false
		}
		for _, t := range m.Spec.Tools {
			if t.Name != req.Tool || (t.Server != "" && t.Server != req.Server) {
				continue
			}
			if toolAllows(t, req) {
				return true
			}
		}
		return false
	case ActionEgress:
		if anyClass(m.Spec.DataClasses.NeverEgress) {
			return false
		}
		for _, d := range m.Spec.Egress.Deny {
			if manifest.HostMatch(d, req.Host) {
				return false
			}
		}
		ok := false
		for _, a := range m.Spec.Egress.Allow {
			if manifest.HostMatch(a, req.Host) {
				ok = true
			}
		}
		if len(m.Spec.Egress.Methods) > 0 {
			found := false
			for _, x := range m.Spec.Egress.Methods {
				if strings.EqualFold(x, req.HTTPMethod) {
					found = true
				}
			}
			ok = ok && found
		}
		return ok
	}
	return true
}

func toolAllows(t manifest.Tool, req Request) bool {
	if len(t.Methods) > 0 && req.Method != "" {
		found := false
		for _, x := range t.Methods {
			if x == req.Method {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	for k := range req.Args {
		if _, ok := t.Args[k]; !ok && !t.ExtraArgs {
			return false
		}
	}
	for n, r := range t.Args {
		v, present := req.Args[n]
		if !present {
			if r.Required {
				return false
			}
			continue
		}
		typ := ArgType(v)
		if r.Type != "" && r.Type != "any" {
			if r.Type == "number" {
				if typ != "integer" && typ != "number" {
					return false
				}
			} else if typ != r.Type {
				return false
			}
		}
		s, isStr := v.(string)
		if len(r.Enum) > 0 {
			if !isStr {
				return false
			}
			found := false
			for _, x := range r.Enum {
				if x == s {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		if len(r.Like) > 0 {
			if !isStr {
				return false
			}
			found := false
			for _, l := range r.Like {
				if manifest.Glob(l, s) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
		for _, d := range r.Deny {
			if isStr && manifest.Glob(d, s) {
				return false
			}
		}
		if r.Min != nil || r.Max != nil {
			f, ok := numeric(v)
			if !ok {
				return false
			}
			if r.Min != nil && milli(f) < milli(*r.Min) {
				return false
			}
			if r.Max != nil && milli(f) > milli(*r.Max) {
				return false
			}
		}
		if r.MaxLength > 0 && argLen(v) > r.MaxLength {
			return false
		}
		if r.Pattern != "" && (!isStr || !regexp.MustCompile(r.Pattern).MatchString(s)) {
			return false
		}
	}
	return true
}

func randomManifest(rng *rand.Rand, id string) *manifest.Manifest {
	words := []string{"a", "b", "ab", "x", "gpt", "claude", "mini", "-", "1"}
	glob := func() string {
		var b strings.Builder
		for i := 0; i < 1+rng.Intn(3); i++ {
			if rng.Intn(3) == 0 {
				b.WriteString("*")
			} else {
				b.WriteString(words[rng.Intn(len(words))])
			}
		}
		return b.String()
	}
	f := func(v float64) *float64 { return &v }
	m := &manifest.Manifest{Metadata: manifest.Metadata{ID: id, Owner: "o", Purpose: "p"}}
	for i := 0; i < 1+rng.Intn(3); i++ {
		m.Spec.Model.Allowed = append(m.Spec.Model.Allowed, glob())
	}
	if rng.Intn(2) == 0 {
		m.Spec.Model.Providers = []string{"openai", "anthropic"}[:1+rng.Intn(2)]
	}
	if rng.Intn(2) == 0 {
		m.Spec.Model.MaxTokensPerRequest = 100 * (1 + rng.Intn(10))
	}
	classes := []string{"pii.ssn", "pci.*", "secret.*", "pii", "financial.customer_table", "*"}
	if rng.Intn(2) == 0 {
		m.Spec.DataClasses.NeverEgress = []string{classes[rng.Intn(len(classes))]}
	}
	if rng.Intn(2) == 0 {
		m.Spec.DataClasses.NeverModel = []string{classes[rng.Intn(len(classes))]}
	}
	servers := []string{"", "s1", "s2"}
	for i := 0; i < rng.Intn(4); i++ {
		t := manifest.Tool{Name: []string{"t1", "t2", "t3"}[rng.Intn(3)], Server: servers[rng.Intn(3)], BlastRadius: "low", ExtraArgs: rng.Intn(4) == 0}
		dup := false
		for _, o := range m.Spec.Tools {
			if o.Name == t.Name && o.Server == t.Server {
				dup = true
			}
		}
		if dup {
			continue
		}
		if rng.Intn(2) == 0 {
			t.Methods = []string{"read", "write"}[:1+rng.Intn(2)]
		}
		t.Args = map[string]manifest.ArgRule{}
		for j := 0; j < rng.Intn(3); j++ {
			r := manifest.ArgRule{Required: rng.Intn(3) == 0}
			switch rng.Intn(6) {
			case 0:
				r.Type = "string"
				r.Enum = []string{"us", "eu"}
			case 1:
				r.Type = "integer"
				r.Min = f(float64(rng.Intn(5)))
				r.Max = f(float64(5 + rng.Intn(10)))
			case 2:
				r.Type = "string"
				r.Like = []string{glob()}
			case 3:
				r.Pattern = "^[a-z]+$"
			case 4:
				r.MaxLength = 1 + rng.Intn(6)
				r.Deny = []string{glob()}
			case 5:
				r.Type = []string{"boolean", "array", "number", "any"}[rng.Intn(4)]
			}
			t.Args[[]string{"p", "q", "r"}[rng.Intn(3)]] = r
		}
		m.Spec.Tools = append(m.Spec.Tools, t)
	}
	hosts := []string{"*.stripe.com", "api.acme.com", "*.io", "x*"}
	for i := 0; i < rng.Intn(3); i++ {
		m.Spec.Egress.Allow = append(m.Spec.Egress.Allow, hosts[rng.Intn(len(hosts))])
	}
	if rng.Intn(3) == 0 {
		m.Spec.Egress.Deny = []string{hosts[rng.Intn(len(hosts))]}
	}
	if rng.Intn(3) == 0 {
		m.Spec.Egress.Methods = []string{"GET"}
	}
	m.ApplyDefaults()
	return m
}

func randomRequest(rng *rand.Rand, id string) Request {
	vals := []any{"us", "eu", "cn", "abc", "ABC", "gptx", "", "a-b", 3.0, 7.0, 12.0, 4.5, json.Number("6"), true, []any{"a"}, map[string]any{"k": "v"}, nil, "xxxxxxxxxxxx"}
	models := []string{"gpt-4o", "claude-3", "ab", "x", "gptmini", "a-1", "bab"}
	req := Request{AgentID: id}
	switch rng.Intn(3) {
	case 0:
		req.Action = ActionLLM
		req.Model = models[rng.Intn(len(models))]
		req.Provider = []string{"openai", "anthropic", "groq"}[rng.Intn(3)]
		req.MaxTokens = rng.Intn(1200)
	case 1:
		req.Action = ActionTool
		req.Tool = []string{"t1", "t2", "t3", "t4"}[rng.Intn(4)]
		req.Server = []string{"", "s1", "s2"}[rng.Intn(3)]
		req.Method = []string{"", "read", "write", "delete"}[rng.Intn(4)]
		req.Args = map[string]any{}
		for i := 0; i < rng.Intn(3); i++ {
			req.Args[[]string{"p", "q", "r", "z"}[rng.Intn(4)]] = vals[rng.Intn(len(vals))]
		}
	case 2:
		req.Action = ActionEgress
		req.Host = []string{"api.stripe.com", "stripe.com", "api.acme.com", "evil.io", "xyz", "a.b.io", "stripe.com.evil"}[rng.Intn(7)]
		req.HTTPMethod = []string{"GET", "POST"}[rng.Intn(2)]
	}
	if rng.Intn(3) == 0 {
		req.DataClasses = []string{[]string{"pii.ssn", "pci.card", "secret.github.pat", "pii.email", "financial.customer_table"}[rng.Intn(5)]}
	}
	return req
}

func TestCompiledNeverGrantsMoreThanManifest(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	checked := 0
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("agent-%d", i)
		m := randomManifest(rng, id)
		if m.Validate() != nil {
			continue
		}
		e := NewEngine()
		if err := e.Load(m); err != nil {
			src, _ := Compile(m)
			t.Fatalf("load: %v\n%s", err, src)
		}
		for j := 0; j < 60; j++ {
			req := randomRequest(rng, id)
			got := e.Evaluate(req).Allowed
			want := reference(m, req)
			checked++
			if got != want {
				src, _ := Compile(m)
				t.Fatalf("mismatch got=%v want=%v req=%+v\n%s", got, want, req, src)
			}
		}
	}
	t.Logf("checked %d decisions", checked)
}

func BenchmarkEvaluateTool(b *testing.B) {
	e, _ := loadExample(b)
	req := Request{AgentID: "finance-agent", Action: ActionTool, Server: "finance-mcp", Tool: "send_email", Args: map[string]any{"to": "cfo@acme.example", "subject": "hello", "body": "quarterly numbers"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		e.Evaluate(req)
	}
}
