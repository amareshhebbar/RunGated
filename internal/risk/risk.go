package risk

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

var DefaultWeights = map[core.Category]float64{
	core.CatPolicy:    1.0,
	core.CatInjection: 1.0,
	core.CatSecret:    1.0,
	core.CatPII:       0.9,
	core.CatPHI:       1.0,
	core.CatEgress:    0.9,
	core.CatRate:      1.0,
	core.CatLoop:      0.9,
	core.CatCost:      1.0,
	core.CatPin:       1.0,
	core.CatExfil:     1.0,
	core.CatJudge:     1.0,
	core.CatContain:   1.0,
	core.CatScope:     1.0,
}

var tierFactor = map[string]float64{"low": 0.85, "medium": 1.0, "high": 1.15, "critical": 1.3}

var sensitivePrefixes = []string{"secret.", "pci.", "phi.", "pii.ssn", "pii.aadhaar", "pii.passport", "data.bulk_records"}

type Input struct {
	Findings      []core.Finding
	Manifest      *manifest.Manifest
	ActionClass   string
	Tool          string
	PolicyAllowed bool
	PolicyChecked bool
	Redactable    func(core.Finding) bool
}

type Output struct {
	Score       int
	Residual    int
	Verdict     core.Verdict
	Reasons     []string
	ToRedact    []core.Finding
	Sensitivity float64
}

func weight(m *manifest.Manifest, c core.Category) float64 {
	if m != nil {
		if w, ok := m.Spec.Risk.Weights[string(c)]; ok {
			return w
		}
	}
	if w, ok := DefaultWeights[c]; ok {
		return w
	}
	return 1
}

func Sensitivity(fs []core.Finding) float64 {
	s := 0.0
	for _, f := range fs {
		for _, p := range sensitivePrefixes {
			if strings.HasPrefix(f.DataClass, p) {
				s = math.Max(s, 0.05)
			}
		}
		if strings.HasPrefix(f.DataClass, "secret.") || f.DataClass == "data.bulk_records" {
			s = math.Max(s, 0.08)
		}
	}
	return s
}

func fuse(m *manifest.Manifest, fs []core.Finding, tier float64, sens float64) int {
	prod := 1.0
	for _, f := range fs {
		v := f.Severity * f.Confidence * weight(m, f.Category)
		if v > 1 {
			v = 1
		}
		if v < 0 {
			v = 0
		}
		prod *= 1 - v
	}
	raw := 1 - prod
	if raw == 0 {
		return 0
	}
	s := math.Min(1, raw*tier+sens)
	return int(math.Round(s * 100))
}

func corroborated(fs []core.Finding) bool {
	cats := map[core.Category]bool{}
	for _, f := range fs {
		if f.Min == core.VerdictContain {
			return true
		}
		if f.Severity*f.Confidence >= 0.5 {
			cats[f.Category] = true
		}
	}
	return len(cats) >= 2
}

func Evaluate(in Input) Output {
	m := in.Manifest
	th := manifest.Thresholds{Warn: 20, Redact: 30, Hold: 60, Block: 80, Contain: 95}
	tier := 1.0
	if m != nil {
		th = m.Spec.Risk.Thresholds
		if f, ok := tierFactor[m.Spec.RiskTier]; ok {
			tier = f
		}
	}
	findings := in.Findings
	if in.PolicyChecked && !in.PolicyAllowed {
		findings = append(findings, core.Finding{Detector: "policy", Category: core.CatPolicy, Rule: "cedar.deny", Severity: 0.9, Confidence: 1, Message: "action outside the agent manifest", Min: core.VerdictBlock})
	}
	var redact, residual []core.Finding
	for _, f := range findings {
		if f.Redactable && (in.Redactable == nil || in.Redactable(f)) {
			redact = append(redact, f)
		} else {
			residual = append(residual, f)
		}
	}
	sens := Sensitivity(findings)
	out := Output{Sensitivity: sens}
	out.Score = fuse(m, findings, tier, sens)
	out.Residual = fuse(m, residual, tier, Sensitivity(residual))
	if out.Score >= th.Contain && !corroborated(findings) {
		out.Score = th.Contain - 1
	}
	if out.Residual >= th.Contain && !corroborated(residual) {
		out.Residual = th.Contain - 1
	}
	v := core.VerdictAllow
	switch {
	case out.Score >= th.Contain:
		v = core.VerdictContain
	case out.Residual >= th.Block:
		v = core.VerdictBlock
	case out.Residual >= th.Hold:
		v = core.VerdictHold
	case len(redact) > 0 && out.Score >= th.Redact:
		v = core.VerdictRedact
	case out.Score >= th.Warn:
		v = core.VerdictWarn
	}
	for _, f := range findings {
		if f.Min != "" {
			v = core.MaxVerdict(v, f.Min)
		}
	}
	out.ToRedact = redact
	out.Verdict = v
	sorted := append([]core.Finding{}, findings...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Severity*sorted[i].Confidence > sorted[j].Severity*sorted[j].Confidence
	})
	seen := map[string]bool{}
	for _, f := range sorted {
		if seen[f.Message] {
			continue
		}
		seen[f.Message] = true
		out.Reasons = append(out.Reasons, f.Message)
		if len(out.Reasons) >= 6 {
			break
		}
	}
	return out
}

func Enforce(computed core.Verdict, mode core.Mode, blast string, policyDenied bool) (core.Verdict, string) {
	switch mode {
	case core.ModeObserve:
		return core.VerdictAllow, "observe mode: logged only"
	case core.ModeRecommend:
		if computed.Rank() > core.VerdictWarn.Rank() {
			return core.VerdictWarn, fmt.Sprintf("recommend mode: would %s", computed)
		}
		return computed, ""
	case core.ModeHuman:
		switch computed {
		case core.VerdictBlock:
			if policyDenied {
				return core.VerdictBlock, ""
			}
			return core.VerdictHold, "human mode: held for approval instead of block"
		case core.VerdictContain:
			return core.VerdictHold, "human mode: held for approval instead of containment"
		}
		return computed, ""
	case core.ModeAutoContain:
		return computed, ""
	case core.ModeFullAuto:
		if computed == core.VerdictHold {
			if blast == "low" {
				return core.VerdictBlock, "full auto: low blast radius hold resolved as block"
			}
			return core.VerdictHold, ""
		}
		return computed, ""
	}
	return computed, ""
}
