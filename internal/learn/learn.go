package learn

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type Options struct {
	AgentID        string
	Owner          string
	Purpose        string
	Tenant         string
	Name           string
	Mode           string
	EnumMax        int
	EnumMinSamples int
	Headroom       float64
}

type Report struct {
	Events      int      `json:"events"`
	Used        int      `json:"used"`
	SkippedRisk int      `json:"skipped_risky"`
	Tools       int      `json:"tools"`
	Models      int      `json:"models"`
	Hosts       int      `json:"hosts"`
	Notes       []string `json:"notes,omitempty"`
}

type argStat struct {
	seen    int
	types   map[string]int
	values  map[string]int
	shapes  map[string]int
	min     float64
	max     float64
	maxLen  int
	numeric int
}

type toolStat struct {
	name    string
	server  string
	calls   int
	args    map[string]*argStat
	noArgs  int
	methods map[string]bool
}

var riskyCats = map[core.Category]bool{
	core.CatInjection: true,
	core.CatSecret:    true,
}

func risky(e core.Event) bool {
	if e.Decision == nil {
		return false
	}
	for _, f := range e.Decision.Findings {
		if riskyCats[f.Category] && f.Severity >= 0.5 {
			return true
		}
	}
	return false
}

func ReadEvents(r io.Reader, agent string) ([]core.Event, error) {
	var out []core.Event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e core.Event
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if agent != "" && e.AgentID != agent {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

func Suggest(events []core.Event, o Options) (*manifest.Manifest, Report) {
	if o.EnumMax == 0 {
		o.EnumMax = 6
	}
	if o.EnumMinSamples == 0 {
		o.EnumMinSamples = 20
	}
	if o.Headroom == 0 {
		o.Headroom = 1.5
	}
	if o.Mode == "" {
		o.Mode = "observe"
	}
	rep := Report{Events: len(events)}
	tools := map[string]*toolStat{}
	models := map[string]bool{}
	providers := map[string]bool{}
	hosts := map[string]bool{}
	servers := map[string]bool{}
	classes := map[string]bool{}
	argsHidden := false
	for _, e := range events {
		if o.AgentID != "" && e.AgentID != o.AgentID {
			continue
		}
		if e.Action == nil {
			continue
		}
		if risky(e) {
			rep.SkippedRisk++
			continue
		}
		rep.Used++
		a := e.Action
		if o.Tenant == "" && e.TenantID != "" {
			o.Tenant = e.TenantID
		}
		if e.Decision != nil {
			for _, f := range e.Decision.Findings {
				if f.DataClass != "" && !strings.HasPrefix(f.DataClass, "untrusted.") {
					classes[f.DataClass] = true
				}
			}
		}
		switch a.Kind {
		case core.KindLLMRequest, core.KindLLMResponse:
			if a.Model != "" {
				models[a.Model] = true
			}
			if a.Provider != "" {
				providers[a.Provider] = true
			}
		case core.KindToolCall:
			if a.Tool == "" {
				continue
			}
			key := a.Server + "\x00" + a.Tool
			t := tools[key]
			if t == nil {
				t = &toolStat{name: a.Tool, server: a.Server, args: map[string]*argStat{}, methods: map[string]bool{}}
				tools[key] = t
			}
			t.calls++
			if a.Method != "" {
				t.methods[a.Method] = true
			}
			if a.Args == nil {
				t.noArgs++
				argsHidden = true
				continue
			}
			for k, v := range a.Args {
				s := t.args[k]
				if s == nil {
					s = &argStat{types: map[string]int{}, values: map[string]int{}, shapes: map[string]int{}, min: math.Inf(1), max: math.Inf(-1)}
					t.args[k] = s
				}
				s.observe(v)
			}
			if a.Server != "" {
				servers[a.Server] = true
			}
		case core.KindEgressRequest:
			if a.Host != "" {
				hosts[strings.ToLower(a.Host)] = true
			}
		}
	}

	m := &manifest.Manifest{APIVersion: manifest.APIVersion, Kind: manifest.Kind}
	m.Metadata = manifest.Metadata{ID: o.AgentID, Name: o.Name, Owner: o.Owner, Purpose: o.Purpose, Tenant: o.Tenant, Version: "0.1.0"}
	if m.Metadata.Owner == "" {
		m.Metadata.Owner = "you@example.com"
	}
	if m.Metadata.Purpose == "" {
		m.Metadata.Purpose = "Describe what this agent does"
	}
	m.Spec.RiskTier = "medium"
	m.Spec.Autonomy.Default = o.Mode
	m.Spec.Model.Providers = sortedKeys(providers)
	m.Spec.Model.Allowed = generalizeModels(sortedKeys(models))
	if len(m.Spec.Model.Allowed) == 0 {
		m.Spec.Model.Allowed = []string{"*"}
		rep.Notes = append(rep.Notes, "no model calls seen: model.allowed set to *, narrow it")
	}
	m.Spec.Egress.Allow = sortedKeys(hosts)
	if cl := sortedKeys(classes); len(cl) > 0 {
		m.Spec.DataClasses.Allowed = cl
		rep.Notes = append(rep.Notes, "dataClasses.allowed lists every data type seen in traffic; remove any this agent should not move")
	}

	keys := make([]string, 0, len(tools))
	for k := range tools {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t := tools[k]
		mt := manifest.Tool{Name: t.name, Server: t.server, BlastRadius: guessBlast(t.name)}
		mt.Description = fmt.Sprintf("Seen %d times", t.calls)
		if len(t.methods) > 0 {
			mt.Methods = sortedKeys(t.methods)
		}
		withArgs := t.calls - t.noArgs
		if withArgs == 0 {
			mt.ExtraArgs = true
		} else {
			mt.Args = map[string]manifest.ArgRule{}
			for name, s := range t.args {
				mt.Args[name] = s.rule(withArgs, o)
			}
		}
		m.Spec.Tools = append(m.Spec.Tools, mt)
	}
	if argsHidden {
		rep.Notes = append(rep.Notes, "some tool calls had no stored arguments (privacy.storeContent is off); those tools allow any arguments")
	}
	if len(m.Spec.Tools) == 0 {
		rep.Notes = append(rep.Notes, "no tool calls seen: run the agent through the gateway first, then run this again")
	}
	rep.Tools, rep.Models, rep.Hosts = len(m.Spec.Tools), len(models), len(hosts)
	return m, rep
}

func (s *argStat) observe(v any) {
	s.seen++
	switch x := v.(type) {
	case nil:
		s.types["null"]++
	case bool:
		s.types["boolean"]++
		s.values[fmt.Sprint(x)]++
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			s.types["integer"]++
		} else {
			s.types["number"]++
		}
		s.num(x)
	case int:
		s.types["integer"]++
		s.num(float64(x))
	case json.Number:
		f, _ := x.Float64()
		s.types["number"]++
		s.num(f)
	case string:
		s.types["string"]++
		if len(s.values) <= 64 {
			s.values[x]++
		}
		s.shapes[shape(x)]++
		if n := len(x); n > s.maxLen {
			s.maxLen = n
		}
	case []any:
		s.types["array"]++
	case map[string]any:
		s.types["object"]++
	default:
		s.types["any"]++
	}
}

func (s *argStat) num(f float64) {
	s.numeric++
	if f < s.min {
		s.min = f
	}
	if f > s.max {
		s.max = f
	}
}

func (s *argStat) rule(calls int, o Options) manifest.ArgRule {
	r := manifest.ArgRule{Required: s.seen == calls && s.types["null"] == 0}
	typ := ""
	for t := range s.types {
		if t == "null" {
			continue
		}
		switch {
		case typ == "":
			typ = t
		case (typ == "integer" && t == "number") || (typ == "number" && t == "integer"):
			typ = "number"
		case typ != t:
			typ = "any"
		}
	}
	if typ == "" {
		typ = "any"
	}
	r.Type = typ
	switch typ {
	case "integer", "number":
		if s.numeric > 0 {
			lo, hi := s.min, s.max
			if lo >= 0 {
				z := 0.0
				r.Min = &z
			} else {
				l := roundOut(lo*o.Headroom, false)
				r.Min = &l
			}
			h := roundOut(hi*o.Headroom, true)
			if hi <= 0 {
				h = 0
			}
			r.Max = &h
		}
	case "string":
		total := 0
		for _, n := range s.values {
			total += n
		}
		if len(s.values) <= o.EnumMax && total >= o.EnumMinSamples && len(s.values) <= 64 {
			r.Enum = sortedKeys(toSet(s.values))
			break
		}
		if len(s.shapes) == 1 {
			for sh := range s.shapes {
				if p := shapePattern(sh); p != "" {
					r.Pattern = p
				}
			}
		}
		if r.Pattern == "" && s.maxLen > 0 {
			r.MaxLength = roundLen(int(float64(s.maxLen) * o.Headroom))
		}
	}
	return r
}

func roundOut(v float64, up bool) float64 {
	if v == 0 {
		return 0
	}
	mag := math.Pow(10, math.Floor(math.Log10(math.Abs(v))))
	step := mag / 2
	if up {
		return math.Ceil(v/step) * step
	}
	return math.Floor(v/step) * step
}

func roundLen(n int) int {
	for _, b := range []int{16, 32, 64, 128, 256, 512, 1024, 2048, 4096, 8192, 16384} {
		if n <= b {
			return b
		}
	}
	return 65536
}

type run struct {
	class rune
	lit   string
	n     int
}

func runs(s string) []run {
	var out []run
	for _, c := range s {
		cl := 'x'
		lit := ""
		switch {
		case c >= '0' && c <= '9':
			cl = 'd'
		case c >= 'A' && c <= 'Z':
			cl = 'U'
		case c >= 'a' && c <= 'z':
			cl = 'l'
		case strings.ContainsRune("-_.:/@#", c):
			cl = 'p'
			lit = string(c)
		default:
			cl = 'x'
		}
		if n := len(out); n > 0 && out[n-1].class == cl && out[n-1].lit == lit && cl != 'p' {
			out[n-1].n++
			continue
		}
		out = append(out, run{class: cl, lit: lit, n: 1})
	}
	return out
}

func shape(s string) string {
	if s == "" || len(s) > 64 {
		return "*"
	}
	rs := runs(s)
	if len(rs) > 8 {
		return "*"
	}
	var b strings.Builder
	upperPrefix := true
	for i, r := range rs {
		switch r.class {
		case 'x':
			return "*"
		case 'p':
			b.WriteString("p" + r.lit)
		case 'U':
			if i == 0 && upperPrefix && r.n <= 6 && len(rs) > 1 {
				b.WriteString("L" + s[:r.n])
			} else {
				fmt.Fprintf(&b, "U%d", r.n)
			}
		default:
			fmt.Fprintf(&b, "%c%d", r.class, r.n)
		}
		b.WriteByte('|')
	}
	return b.String()
}

var shapeTok = regexp.MustCompile(`^([dUlLp])(.*)$`)

func shapePattern(sh string) string {
	if sh == "*" || sh == "" {
		return ""
	}
	parts := strings.Split(strings.TrimSuffix(sh, "|"), "|")
	if len(parts) < 2 {
		if strings.HasPrefix(sh, "d") {
			return "^" + seg(parts[0]) + "$"
		}
		return ""
	}
	var b strings.Builder
	b.WriteByte('^')
	hasFixed := false
	for _, p := range parts {
		m := shapeTok.FindStringSubmatch(p)
		if m == nil {
			return ""
		}
		if m[1] == "L" || m[1] == "p" {
			hasFixed = true
		}
		b.WriteString(seg(p))
	}
	if !hasFixed {
		return ""
	}
	b.WriteByte('$')
	return b.String()
}

func seg(p string) string {
	kind, rest := p[:1], p[1:]
	switch kind {
	case "L":
		return regexp.QuoteMeta(rest)
	case "p":
		return regexp.QuoteMeta(rest)
	case "d":
		return "[0-9]{" + rest + "}"
	case "U":
		return "[A-Z]{" + rest + "}"
	case "l":
		return "[a-z]{" + rest + "}"
	}
	return ""
}

var highWords = []string{"delete", "remove", "drop", "refund", "pay", "transfer", "send", "email", "post", "write", "update", "create", "exec", "run", "shell", "deploy", "grant", "invite", "charge", "wire", "upload", "publish", "purge", "kill"}
var lowWords = []string{"get", "list", "read", "search", "lookup", "find", "fetch", "describe", "query", "show", "view", "status", "count"}

func guessBlast(name string) string {
	words := splitWords(name)
	for _, w := range words {
		for _, h := range highWords {
			if w == h {
				return "high"
			}
		}
	}
	for _, w := range words {
		for _, l := range lowWords {
			if w == l {
				return "low"
			}
		}
	}
	return "medium"
}

func splitWords(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	prevLower := false
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			flush()
			prevLower = false
			continue
		}
		if unicode.IsUpper(c) && prevLower {
			flush()
		}
		cur = append(cur, c)
		prevLower = unicode.IsLower(c)
	}
	flush()
	return out
}

var modelVer = regexp.MustCompile(`^(.*?)-(\d{4}-\d{2}-\d{2}|\d{8})$`)

func generalizeModels(ms []string) []string {
	set := map[string]bool{}
	for _, m := range ms {
		if g := modelVer.FindStringSubmatch(m); g != nil {
			set[g[1]+"*"] = true
			continue
		}
		set[m] = true
	}
	return sortedKeys(set)
}

func toSet(m map[string]int) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
