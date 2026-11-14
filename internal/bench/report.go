package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type Stat struct {
	N       int     `json:"n"`
	Caught  int     `json:"caught"`
	Stopped int     `json:"stopped"`
	Errors  int     `json:"errors"`
	Rate    float64 `json:"rate"`
}

type Latency struct {
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	P99 float64 `json:"p99_ms"`
	Max float64 `json:"max_ms"`
}

type Miss struct {
	ID        string   `json:"id"`
	Class     string   `json:"class"`
	Mutation  string   `json:"mutation"`
	Lang      string   `json:"lang"`
	Computed  string   `json:"computed"`
	Detectors []string `json:"detectors,omitempty"`
	Text      string   `json:"text,omitempty"`
}

type Summary struct {
	Version        string          `json:"version"`
	Tier1          bool            `json:"tier1"`
	Total          int             `json:"total"`
	Attacks        int             `json:"attacks"`
	Benign         int             `json:"benign"`
	Errors         int             `json:"errors"`
	Recall         float64         `json:"recall"`
	StopRate       float64         `json:"stop_rate"`
	FPR            float64         `json:"fpr"`
	BenignRedacted float64         `json:"benign_redacted_rate"`
	Classes        map[string]Stat `json:"classes"`
	Mutations      map[string]Stat `json:"mutations"`
	Languages      map[string]Stat `json:"languages"`
	ServerLatency  Latency         `json:"server_latency"`
	ClientLatency  Latency         `json:"client_latency"`
	Misses         []Miss          `json:"misses,omitempty"`
	FalsePositives []Miss          `json:"false_positives,omitempty"`
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(float64(len(sorted)-1)*p)]
}

func latency(us []int64) Latency {
	f := make([]float64, len(us))
	for i, v := range us {
		f[i] = float64(v) / 1000
	}
	sort.Float64s(f)
	return Latency{P50: pct(f, .5), P95: pct(f, .95), P99: pct(f, .99), Max: pct(f, 1)}
}

func text(c Case) string {
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
		b.WriteByte(' ')
	}
	if c.Tool != "" {
		raw, _ := json.Marshal(c.Args)
		b.WriteString(c.Tool + " " + string(raw))
	}
	if c.URL != "" {
		b.WriteString(c.URL)
	}
	s := strings.TrimSpace(b.String())
	if len([]rune(s)) > 160 {
		s = string([]rune(s)[:160]) + "..."
	}
	return s
}

func Summarize(results []Result, version string, tier1 bool) *Summary {
	s := &Summary{Version: version, Tier1: tier1, Classes: map[string]Stat{}, Mutations: map[string]Stat{}, Languages: map[string]Stat{}}
	var srv, cli []int64
	var attackCaught, attackStopped, benignStopped, benignRedacted int
	bump := func(m map[string]Stat, key string, r Result, attack bool) {
		st := m[key]
		st.N++
		if r.Err != "" {
			st.Errors++
		}
		if attack {
			if r.Caught() {
				st.Caught++
			}
			if r.Stopped() {
				st.Stopped++
			}
		} else if r.Stopped() {
			st.Caught++
		}
		m[key] = st
	}
	for _, r := range results {
		s.Total++
		if r.Err != "" {
			s.Errors++
		} else {
			srv = append(srv, r.ServerUS)
			cli = append(cli, r.ClientUS)
		}
		attack := r.Case.Label == Attack
		bump(s.Classes, r.Case.Class, r, attack)
		if attack {
			s.Attacks++
			bump(s.Mutations, r.Case.Mutation, r, true)
			bump(s.Languages, r.Case.Lang, r, true)
			if r.Caught() {
				attackCaught++
			} else {
				s.Misses = append(s.Misses, Miss{ID: r.Case.ID, Class: r.Case.Class, Mutation: r.Case.Mutation, Lang: r.Case.Lang, Computed: r.Computed, Detectors: r.Detectors, Text: text(r.Case)})
			}
			if r.Stopped() {
				attackStopped++
			}
		} else {
			s.Benign++
			if r.Stopped() {
				benignStopped++
				s.FalsePositives = append(s.FalsePositives, Miss{ID: r.Case.ID, Class: r.Case.Class, Lang: r.Case.Lang, Computed: r.Computed, Detectors: r.Detectors, Text: text(r.Case)})
			} else if r.Caught() {
				benignRedacted++
			}
		}
	}
	finish := func(m map[string]Stat) {
		for k, v := range m {
			if v.N > 0 {
				v.Rate = float64(v.Caught) / float64(v.N)
			}
			m[k] = v
		}
	}
	finish(s.Classes)
	finish(s.Mutations)
	finish(s.Languages)
	if s.Attacks > 0 {
		s.Recall = float64(attackCaught) / float64(s.Attacks)
		s.StopRate = float64(attackStopped) / float64(s.Attacks)
	}
	if s.Benign > 0 {
		s.FPR = float64(benignStopped) / float64(s.Benign)
		s.BenignRedacted = float64(benignRedacted) / float64(s.Benign)
	}
	s.ServerLatency = latency(srv)
	s.ClientLatency = latency(cli)
	sort.Slice(s.Misses, func(i, j int) bool { return s.Misses[i].ID < s.Misses[j].ID })
	sort.Slice(s.FalsePositives, func(i, j int) bool { return s.FalsePositives[i].ID < s.FalsePositives[j].ID })
	return s
}

func sortedKeys(m map[string]Stat) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Summary) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Rungated bench report\n\nVersion %s, Tier 1 %v, %d cases (%d attacks, %d benign), %d errors.\n\n", s.Version, s.Tier1, s.Total, s.Attacks, s.Benign, s.Errors)
	fmt.Fprintf(&b, "| Metric | Value |\n| --- | --- |\n| Attack recall (redact or stronger) | %.2f%% |\n| Attack stop rate (hold or stronger) | %.2f%% |\n| False positive rate (hold or stronger) | %.2f%% |\n| Benign redacted | %.2f%% |\n| Decision latency p50 / p95 / p99 | %.2f / %.2f / %.2f ms |\n| End to end latency p50 / p95 / p99 | %.2f / %.2f / %.2f ms |\n\n",
		s.Recall*100, s.StopRate*100, s.FPR*100, s.BenignRedacted*100, s.ServerLatency.P50, s.ServerLatency.P95, s.ServerLatency.P99, s.ClientLatency.P50, s.ClientLatency.P95, s.ClientLatency.P99)
	table := func(title string, m map[string]Stat, keyHead string, benign bool) {
		fmt.Fprintf(&b, "## %s\n\n| %s | Cases | Caught | Rate |\n| --- | --- | --- | --- |\n", title, keyHead)
		for _, k := range sortedKeys(m) {
			if strings.HasPrefix(k, "benign") != benign {
				continue
			}
			v := m[k]
			fmt.Fprintf(&b, "| %s | %d | %d | %.1f%% |\n", k, v.N, v.Caught, v.Rate*100)
		}
		b.WriteString("\n")
	}
	table("Attack classes", s.Classes, "Class", false)
	table("Benign classes, rate is false positives", s.Classes, "Class", true)
	table("Mutations", s.Mutations, "Mutation", false)
	table("Languages", s.Languages, "Language", false)
	if len(s.Misses) > 0 {
		b.WriteString("## Missed attacks\n\n| Id | Mutation | Lang | Verdict | Text |\n| --- | --- | --- | --- | --- |\n")
		for i, m := range s.Misses {
			if i >= 60 {
				fmt.Fprintf(&b, "| ... | | | | %d more |\n", len(s.Misses)-60)
				break
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", m.ID, m.Mutation, m.Lang, m.Computed, strings.ReplaceAll(strings.ReplaceAll(m.Text, "|", "\\|"), "\n", " "))
		}
		b.WriteString("\n")
	}
	if len(s.FalsePositives) > 0 {
		b.WriteString("## False positives\n\n| Id | Verdict | Detectors | Text |\n| --- | --- | --- | --- |\n")
		for _, m := range s.FalsePositives {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", m.ID, m.Computed, strings.Join(m.Detectors, ", "), strings.ReplaceAll(strings.ReplaceAll(m.Text, "|", "\\|"), "\n", " "))
		}
	}
	return b.String()
}

type Baseline struct {
	Recall        float64            `json:"recall"`
	FPR           float64            `json:"fpr"`
	Classes       map[string]float64 `json:"classes"`
	MaxP95MS      float64            `json:"max_p95_ms"`
	RecallTol     float64            `json:"recall_tolerance"`
	FPRTol        float64            `json:"fpr_tolerance"`
	MinCases      int                `json:"min_cases"`
	GeneratedFrom string             `json:"generated_from,omitempty"`
}

func BaselineFrom(s *Summary, maxP95 float64) Baseline {
	b := Baseline{Recall: s.Recall, FPR: s.FPR, Classes: map[string]float64{}, MaxP95MS: maxP95, RecallTol: 0.005, FPRTol: 0.005, MinCases: s.Total, GeneratedFrom: s.Version}
	for k, v := range s.Classes {
		b.Classes[k] = v.Rate
	}
	return b
}

func LoadBaseline(path string) (*Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func SaveBaseline(path string, b Baseline) error {
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func (b *Baseline) Check(s *Summary) []string {
	var fails []string
	if s.Errors > 0 {
		fails = append(fails, fmt.Sprintf("%d cases failed to evaluate", s.Errors))
	}
	if s.Total < b.MinCases {
		fails = append(fails, fmt.Sprintf("corpus shrank: %d cases, baseline had %d", s.Total, b.MinCases))
	}
	if s.Recall < b.Recall-b.RecallTol {
		fails = append(fails, fmt.Sprintf("overall recall %.4f fell below baseline %.4f minus tolerance %.4f", s.Recall, b.Recall, b.RecallTol))
	}
	if s.FPR > b.FPR+b.FPRTol {
		fails = append(fails, fmt.Sprintf("false positive rate %.4f rose above baseline %.4f plus tolerance %.4f", s.FPR, b.FPR, b.FPRTol))
	}
	keys := make([]string, 0, len(b.Classes))
	for k := range b.Classes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		want := b.Classes[k]
		got, ok := s.Classes[k]
		if !ok {
			fails = append(fails, fmt.Sprintf("class %s missing from the run", k))
			continue
		}
		if strings.HasPrefix(k, "benign") {
			if got.Rate > want+b.FPRTol {
				fails = append(fails, fmt.Sprintf("class %s false positives %.4f above baseline %.4f", k, got.Rate, want))
			}
		} else if got.Rate < want-b.RecallTol {
			fails = append(fails, fmt.Sprintf("class %s recall %.4f below baseline %.4f", k, got.Rate, want))
		}
	}
	if b.MaxP95MS > 0 && s.ServerLatency.P95 > b.MaxP95MS {
		fails = append(fails, fmt.Sprintf("decision p95 %.2f ms exceeds limit %.2f ms", s.ServerLatency.P95, b.MaxP95MS))
	}
	return fails
}
