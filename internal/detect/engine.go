package detect

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type Classifier interface {
	Name() string
	Classify(ctx context.Context, texts []string) ([]float64, error)
}

type Entity struct {
	Label string
	Start int
	End   int
	Score float64
	Text  string
}

type NER interface {
	Name() string
	Extract(ctx context.Context, text string) ([]Entity, error)
}

type Engine struct {
	Secrets             SecretsDetector
	PII                 PIIDetector
	Injection           InjectionDetector
	Limiter             *Limiter
	Classifier          Classifier
	NER                 NER
	Tier1Timeout        time.Duration
	ClassifierThreshold float64
	NERLabels           map[string]string
	MaxScanBytes        int
}

func NewEngine() *Engine {
	return &Engine{
		Limiter:             NewLimiter(),
		Tier1Timeout:        40 * time.Millisecond,
		ClassifierThreshold: 0.5,
		MaxScanBytes:        512 * 1024,
		NERLabels: map[string]string{
			"person": "pii.person", "per": "pii.person", "name": "pii.person",
			"address": "pii.address", "street address": "pii.address", "loc": "pii.location", "location": "pii.location",
			"organization": "pii.org", "org": "pii.org",
			"phone number": "pii.phone", "email": "pii.email", "email address": "pii.email",
			"date of birth": "pii.dob", "medical condition": "phi.condition", "medication": "phi.medication",
			"credit card number": "pci.card", "social security number": "pii.ssn", "passport number": "pii.passport",
			"bank account number": "pii.bank_account", "ip address": "pii.ip",
		},
	}
}

type Result struct {
	Findings    []core.Finding
	Degraded    bool
	Errors      []string
	Tier1Micros int64
}

func (e *Engine) scanText(t string) string {
	if e.MaxScanBytes > 0 && len(t) > e.MaxScanBytes {
		return t[:e.MaxScanBytes]
	}
	return t
}

func scansInjection(a *core.Action, src core.Source) bool {
	switch a.Kind {
	case core.KindLLMRequest, core.KindToolResult, core.KindEgressResponse, core.KindMCPToolsList, core.KindMCPResource, core.KindMCPPrompt:
		return src.Untrusted()
	case core.KindToolCall:
		return false
	}
	return false
}

func scansData(a *core.Action, src core.Source) bool {
	switch a.Kind {
	case core.KindLLMRequest:
		return src != core.SourceToolDesc
	case core.KindToolCall, core.KindEgressRequest:
		return true
	case core.KindLLMResponse:
		return src == core.SourceAssistant || src == core.SourceToolArgs
	case core.KindToolResult, core.KindEgressResponse, core.KindMCPResource:
		return true
	}
	return false
}

func quarantinable(a *core.Action, src core.Source) bool {
	switch a.Kind {
	case core.KindLLMRequest:
		return src == core.SourceToolResult || src == core.SourceWeb || src == core.SourceRAG || src == core.SourceFile
	case core.KindToolResult, core.KindEgressResponse, core.KindMCPResource, core.KindMCPPrompt:
		return true
	}
	return false
}

func (e *Engine) Run(ctx context.Context, a *core.Action, m *manifest.Manifest) Result {
	var res Result
	var injTexts []string
	var injParts []int
	var nerParts []int
	for i, p := range a.Texts {
		text := e.scanText(p.Text)
		if scansData(a, p.Source) {
			res.Findings = append(res.Findings, e.Secrets.Scan(i, text)...)
			res.Findings = append(res.Findings, e.PII.Scan(i, text)...)
			if e.NER != nil && (a.Kind == core.KindLLMRequest || a.Kind == core.KindToolCall || a.Kind == core.KindEgressRequest) && p.Source != core.SourceAssistant && p.Source != core.SourceSystem {
				nerParts = append(nerParts, i)
			}
		}
		if scansInjection(a, p.Source) {
			fs := e.Injection.Scan(i, p.Source, text)
			for k := range fs {
				if quarantinable(a, p.Source) {
					fs[k].Redactable = true
					fs[k].DataClass = "untrusted.injection"
				}
			}
			res.Findings = append(res.Findings, fs...)
			if e.Classifier != nil {
				injTexts = append(injTexts, text)
				injParts = append(injParts, i)
			}
		}
	}
	res.Findings = append(res.Findings, e.scanJoined(a)...)
	if m != nil {
		if a.Kind == core.KindLLMResponse || a.Kind == core.KindToolCall {
			res.Findings = append(res.Findings, ExfilScan(a, m)...)
		}
		if e.Limiter != nil {
			res.Findings = append(res.Findings, e.Limiter.Check(a, m)...)
		}
	}
	if len(injTexts) == 0 && len(nerParts) == 0 {
		return res
	}
	start := time.Now()
	tctx, cancel := context.WithTimeout(ctx, e.Tier1Timeout)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	if len(injTexts) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			probs, err := e.Classifier.Classify(tctx, injTexts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Degraded = true
				res.Errors = append(res.Errors, e.Classifier.Name()+": "+err.Error())
				return
			}
			for k, pr := range probs {
				if pr < e.ClassifierThreshold {
					continue
				}
				idx := injParts[k]
				src := a.Texts[idx].Source
				f := core.Finding{Detector: e.Classifier.Name(), Tier: 1, Category: core.CatInjection, Rule: "classifier", Severity: pr, Confidence: 0.9, Message: "prompt injection classifier flagged " + string(src) + " content", Span: &core.Span{Part: idx, Start: 0, End: len(a.Texts[idx].Text)}}
				if src == core.SourceUser {
					f.Severity *= 0.85
				}
				if quarantinable(a, src) {
					f.Redactable = true
					f.DataClass = "untrusted.injection"
				}
				res.Findings = append(res.Findings, f)
			}
		}()
	}
	if len(nerParts) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, idx := range nerParts {
				ents, err := e.NER.Extract(tctx, e.scanText(a.Texts[idx].Text))
				mu.Lock()
				if err != nil {
					res.Degraded = true
					res.Errors = append(res.Errors, e.NER.Name()+": "+err.Error())
					mu.Unlock()
					return
				}
				for _, en := range ents {
					cls := e.NERLabels[strings.ToLower(en.Label)]
					if cls == "" {
						cls = "pii." + strings.ReplaceAll(strings.ToLower(en.Label), " ", "_")
					}
					sev := 0.3
					if strings.HasPrefix(cls, "phi.") || cls == "pii.ssn" || cls == "pci.card" {
						sev = 0.7
					}
					res.Findings = append(res.Findings, core.Finding{Detector: e.NER.Name(), Tier: 1, Category: categoryOf(cls), Rule: "ner." + en.Label, Severity: sev, Confidence: math.Min(0.99, en.Score), Message: "entity detected: " + cls, DataClass: cls, Span: &core.Span{Part: idx, Start: en.Start, End: en.End}, Redactable: true, Match: en.Text})
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if errors.Is(tctx.Err(), context.DeadlineExceeded) && !res.Degraded {
		res.Degraded = true
		res.Errors = append(res.Errors, "tier1 timeout")
	}
	res.Tier1Micros = time.Since(start).Microseconds()
	res.Findings = dedupeSpans(res.Findings)
	return res
}

func categoryOf(class string) core.Category {
	switch {
	case strings.HasPrefix(class, "phi."):
		return core.CatPHI
	case strings.HasPrefix(class, "secret."):
		return core.CatSecret
	}
	return core.CatPII
}

func dedupeSpans(fs []core.Finding) []core.Finding {
	out := make([]core.Finding, 0, len(fs))
	for i, f := range fs {
		if f.Tier == 1 && f.Span != nil && f.Redactable && f.DataClass != "untrusted.injection" {
			dup := false
			for j, g := range fs {
				if j != i && g.Tier == 0 && g.Span != nil && g.Span.Part == f.Span.Part && g.Span.Start < f.Span.End && g.Span.End > f.Span.Start {
					dup = true
					break
				}
			}
			if dup {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

func (e *Engine) scanJoined(a *core.Action) []core.Finding {
	var idx []int
	for i, p := range a.Texts {
		if scansInjection(a, p.Source) {
			idx = append(idx, i)
		}
	}
	if len(idx) < 2 {
		return nil
	}
	var out []core.Finding
	for start := 0; start < len(idx); {
		end := start
		for end+1 < len(idx) && idx[end+1] == idx[end]+1 && a.Texts[idx[end+1]].Source == a.Texts[idx[start]].Source {
			end++
		}
		if end > start {
			out = append(out, e.joinedRun(a, idx[start:end+1])...)
		}
		start = end + 1
	}
	return out
}

func (e *Engine) joinedRun(a *core.Action, parts []int) []core.Finding {
	th := e.Injection.Threshold
	if th == 0 {
		th = 0.45
	}
	for _, i := range parts {
		if ScoreInjection(e.scanText(a.Texts[i].Text)).Score >= th {
			return nil
		}
	}
	var b strings.Builder
	for k, i := range parts {
		if k > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(a.Texts[i].Text)
	}
	src := a.Texts[parts[0]].Source
	fs := e.Injection.Scan(parts[0], src, e.scanText(b.String()))
	if len(fs) == 0 {
		return nil
	}
	f := fs[0]
	out := make([]core.Finding, 0, len(parts))
	for _, i := range parts {
		g := f
		g.Rule = "joined:" + f.Rule
		g.Message = "prompt injection split across message parts: " + f.Rule
		g.Span = &core.Span{Part: i, Start: 0, End: len(a.Texts[i].Text)}
		if quarantinable(a, src) {
			g.Redactable = true
			g.DataClass = "untrusted.injection"
		}
		out = append(out, g)
	}
	return out
}
