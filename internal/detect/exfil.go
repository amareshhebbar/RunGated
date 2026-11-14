package detect

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

var mdImageRe = regexp.MustCompile(`!\[[^\]]{0,200}\]\(\s*(https?://[^)\s]+)`)
var urlRe = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

func hostAllowed(m *manifest.Manifest, host string) bool {
	for _, d := range m.Spec.Egress.Deny {
		if manifest.HostMatch(d, host) {
			return false
		}
	}
	for _, a := range m.Spec.Egress.Allow {
		if manifest.HostMatch(a, host) {
			return true
		}
	}
	return false
}

func ExfilScan(a *core.Action, m *manifest.Manifest) []core.Finding {
	var out []core.Finding
	for i, p := range a.Texts {
		switch p.Source {
		case core.SourceAssistant:
			for _, mm := range mdImageRe.FindAllStringSubmatch(p.Text, 16) {
				u, err := url.Parse(mm[1])
				if err != nil || hostAllowed(m, u.Hostname()) {
					continue
				}
				if len(u.RawQuery) > 0 || len(u.Path) > 40 {
					out = append(out, core.Finding{Detector: "exfil", Tier: 0, Category: core.CatExfil, Rule: "markdown_image_beacon", Severity: 0.85, Confidence: 0.85, Message: "model output renders an image from an unapproved host with data in the URL: " + u.Hostname(), Span: &core.Span{Part: i}})
				}
			}
		case core.SourceToolArgs:
			for _, raw := range urlRe.FindAllString(p.Text, 16) {
				u, err := url.Parse(raw)
				if err != nil {
					continue
				}
				h := u.Hostname()
				if hostAllowed(m, h) {
					continue
				}
				sev := 0.45
				if len(u.RawQuery) > 64 || strings.Contains(u.RawQuery, "%") && len(u.RawQuery) > 32 {
					sev = 0.8
				}
				out = append(out, core.Finding{Detector: "exfil", Tier: 0, Category: core.CatEgress, Rule: "tool_arg_unapproved_destination", Severity: sev, Confidence: 0.8, Message: "tool arguments reference an unapproved destination: " + h, Span: &core.Span{Part: i}})
			}
		}
	}
	return out
}

func canonical(v json.RawMessage) []byte {
	if len(bytes.TrimSpace(v)) == 0 {
		return nil
	}
	var x any
	if err := json.Unmarshal(v, &x); err != nil {
		return v
	}
	b, _ := json.Marshal(sortKeys(x))
	return b
}

func sortKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			out[k] = sortKeys(t[k])
		}
		return out
	case []any:
		for i := range t {
			t[i] = sortKeys(t[i])
		}
	}
	return v
}

func ToolFingerprint(name, description string, schema json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte{0})
	h.Write([]byte(description))
	h.Write([]byte{0})
	h.Write(canonical(schema))
	return hex.EncodeToString(h.Sum(nil))
}
