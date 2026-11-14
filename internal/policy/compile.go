package policy

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/amareshhebbar/rungated/pkg/manifest"
)

const (
	ActionLLM    = "llm.call"
	ActionTool   = "tool.call"
	ActionEgress = "egress"
)

func cedarQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case 0:
			b.WriteString(`\0`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func likeQuote(p string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range p {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func setLit(vals []string) string {
	q := make([]string, len(vals))
	for i, v := range vals {
		q[i] = cedarQuote(v)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

func ToolResourceID(server, tool string) string {
	if server == "" {
		server = "_"
	}
	return server + "/" + tool
}

func hostCond(expr string, pattern string) string {
	pattern = strings.ToLower(pattern)
	if strings.HasPrefix(pattern, "*.") {
		return fmt.Sprintf("(%s == %s || %s like %s)", expr, cedarQuote(pattern[2:]), expr, likeQuote(pattern))
	}
	return fmt.Sprintf("%s like %s", expr, likeQuote(pattern))
}

func milli(f float64) int64 {
	return int64(math.Round(f * 1000))
}

func argCond(toolKey, name string, r manifest.ArgRule) string {
	a := fmt.Sprintf("context.args[%s]", cedarQuote(name))
	t := fmt.Sprintf("context.arg_types[%s]", cedarQuote(name))
	var conds []string
	switch r.Type {
	case "", "any":
	case "number":
		conds = append(conds, fmt.Sprintf("(%s == \"integer\" || %s == \"number\")", t, t))
	default:
		conds = append(conds, fmt.Sprintf("%s == %s", t, cedarQuote(r.Type)))
	}
	if len(r.Enum) > 0 {
		conds = append(conds, fmt.Sprintf("%s == \"string\"", t), fmt.Sprintf("%s.contains(%s)", setLit(r.Enum), a))
	}
	if len(r.Like) > 0 {
		var ors []string
		for _, l := range r.Like {
			ors = append(ors, fmt.Sprintf("%s like %s", a, likeQuote(l)))
		}
		conds = append(conds, fmt.Sprintf("%s == \"string\"", t), "("+strings.Join(ors, " || ")+")")
	}
	for _, d := range r.Deny {
		conds = append(conds, fmt.Sprintf("(%s != \"string\" || !(%s like %s))", t, a, likeQuote(d)))
	}
	if r.Min != nil || r.Max != nil {
		n := fmt.Sprintf("context.arg_milli[%s]", cedarQuote(name))
		conds = append(conds, fmt.Sprintf("context.arg_milli has %s", cedarQuote(name)))
		if r.Min != nil {
			conds = append(conds, fmt.Sprintf("%s >= %d", n, milli(*r.Min)))
		}
		if r.Max != nil {
			conds = append(conds, fmt.Sprintf("%s <= %d", n, milli(*r.Max)))
		}
	}
	if r.MaxLength > 0 {
		conds = append(conds, fmt.Sprintf("context.arg_len[%s] <= %d", cedarQuote(name), r.MaxLength))
	}
	if r.Pattern != "" {
		conds = append(conds, fmt.Sprintf("context.arg_regex[%s] == true", cedarQuote(toolKey+":"+name)))
	}
	present := fmt.Sprintf("context.arg_names.contains(%s)", cedarQuote(name))
	body := strings.Join(conds, " && ")
	if r.Required {
		if body == "" {
			return present
		}
		return "(" + present + " && " + body + ")"
	}
	if body == "" {
		return ""
	}
	return fmt.Sprintf("(!(%s) || (%s))", present, body)
}

func Compile(m *manifest.Manifest) (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	agent := fmt.Sprintf("Agent::%s", cedarQuote(m.Metadata.ID))
	id := m.Metadata.ID
	ann := func(name string) {
		fmt.Fprintf(&b, "@id(%s)\n", cedarQuote(id+":"+name))
	}

	var modelOrs []string
	for _, p := range m.Spec.Model.Allowed {
		modelOrs = append(modelOrs, fmt.Sprintf("context.model like %s", likeQuote(p)))
	}
	conds := []string{"(" + strings.Join(modelOrs, " || ") + ")"}
	if len(m.Spec.Model.Providers) > 0 {
		conds = append(conds, fmt.Sprintf("%s.contains(context.provider)", setLit(m.Spec.Model.Providers)))
	}
	if m.Spec.Model.MaxTokensPerRequest > 0 {
		conds = append(conds, fmt.Sprintf("context.max_tokens <= %d", m.Spec.Model.MaxTokensPerRequest))
	}
	ann("model")
	fmt.Fprintf(&b, "permit (principal == %s, action == Action::%s, resource)\nwhen { %s };\n\n", agent, cedarQuote(ActionLLM), strings.Join(conds, " && "))

	tools := append([]manifest.Tool{}, m.Spec.Tools...)
	sort.SliceStable(tools, func(i, j int) bool {
		return ToolResourceID(tools[i].Server, tools[i].Name) < ToolResourceID(tools[j].Server, tools[j].Name)
	})
	for _, t := range tools {
		var tc []string
		resource := "resource"
		if t.Server != "" {
			resource = fmt.Sprintf("resource == Tool::%s", cedarQuote(ToolResourceID(t.Server, t.Name)))
		} else {
			tc = append(tc, fmt.Sprintf("context.tool == %s", cedarQuote(t.Name)))
		}
		if len(t.Methods) > 0 {
			tc = append(tc, fmt.Sprintf("(context.method == \"\" || %s.contains(context.method))", setLit(t.Methods)))
		}
		names := make([]string, 0, len(t.Args))
		for n := range t.Args {
			names = append(names, n)
		}
		sort.Strings(names)
		if !t.ExtraArgs {
			tc = append(tc, fmt.Sprintf("%s.containsAll(context.arg_names)", setLit(names)))
		}
		for _, n := range names {
			if c := argCond(ToolResourceID(t.Server, t.Name), n, t.Args[n]); c != "" {
				tc = append(tc, c)
			}
		}
		ann("tool:" + ToolResourceID(t.Server, t.Name))
		if len(tc) == 0 {
			tc = []string{"true"}
		}
		fmt.Fprintf(&b, "permit (principal == %s, action == Action::%s, %s)\nwhen { %s };\n\n", agent, cedarQuote(ActionTool), resource, strings.Join(tc, " && "))
	}

	if len(m.Spec.Egress.Allow) > 0 {
		var ors []string
		for _, h := range m.Spec.Egress.Allow {
			ors = append(ors, hostCond("context.host", h))
		}
		ec := []string{"(" + strings.Join(ors, " || ") + ")"}
		if len(m.Spec.Egress.Methods) > 0 {
			up := make([]string, len(m.Spec.Egress.Methods))
			for i, x := range m.Spec.Egress.Methods {
				up[i] = strings.ToUpper(x)
			}
			ec = append(ec, fmt.Sprintf("%s.contains(context.http_method)", setLit(up)))
		}
		ann("egress.allow")
		fmt.Fprintf(&b, "permit (principal == %s, action == Action::%s, resource)\nwhen { %s };\n\n", agent, cedarQuote(ActionEgress), strings.Join(ec, " && "))
	}
	for i, h := range m.Spec.Egress.Deny {
		ann(fmt.Sprintf("egress.deny.%d", i))
		fmt.Fprintf(&b, "forbid (principal == %s, action == Action::%s, resource)\nwhen { %s };\n\n", agent, cedarQuote(ActionEgress), hostCond("context.host", h))
	}
	if len(m.Spec.DataClasses.NeverEgress) > 0 {
		ann("data.never_egress")
		fmt.Fprintf(&b, "forbid (principal == %s, action in [Action::%s, Action::%s], resource)\nwhen { context.data_classes.containsAny(%s) };\n\n", agent, cedarQuote(ActionEgress), cedarQuote(ActionTool), setLit(m.Spec.DataClasses.NeverEgress))
	}
	if len(m.Spec.DataClasses.NeverModel) > 0 {
		ann("data.never_model")
		fmt.Fprintf(&b, "forbid (principal == %s, action == Action::%s, resource)\nwhen { context.data_classes.containsAny(%s) };\n\n", agent, cedarQuote(ActionLLM), setLit(m.Spec.DataClasses.NeverModel))
	}
	return b.String(), nil
}

func ExpandClasses(classes []string) []string {
	set := map[string]bool{}
	for _, c := range classes {
		if c == "" {
			continue
		}
		set["*"] = true
		parts := strings.Split(c, ".")
		for i := 1; i <= len(parts); i++ {
			p := strings.Join(parts[:i], ".")
			set[p] = true
			if i < len(parts) {
				set[p+".*"] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
