package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

var buckets = []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 120}

type hist struct {
	counts []uint64
	sum    float64
	n      uint64
}

func newHist() *hist { return &hist{counts: make([]uint64, len(buckets))} }

func (h *hist) observe(v float64) {
	h.sum += v
	h.n++
	for i, b := range buckets {
		if v <= b {
			h.counts[i]++
		}
	}
}

type Metrics struct {
	mu        sync.Mutex
	decisions map[[2]string]uint64
	findings  map[string]uint64
	proxy     map[[2]string]uint64
	decLat    *hist
	addLat    *hist
	upLat     *hist
	hists     map[string]*hist
	gauges    map[string]func() float64
	started   time.Time
}

func New() *Metrics {
	return &Metrics{
		decisions: map[[2]string]uint64{}, findings: map[string]uint64{}, proxy: map[[2]string]uint64{},
		decLat: newHist(), addLat: newHist(), upLat: newHist(), hists: map[string]*hist{}, gauges: map[string]func() float64{}, started: time.Now(),
	}
}

func (m *Metrics) Gauge(name string, fn func() float64) {
	m.mu.Lock()
	m.gauges[name] = fn
	m.mu.Unlock()
}

func (m *Metrics) ObserveDecision(a *core.Action, d *core.Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions[[2]string{string(d.Verdict), string(a.Kind)}]++
	seen := map[core.Category]bool{}
	for _, f := range d.Findings {
		if !seen[f.Category] {
			seen[f.Category] = true
			m.findings[string(f.Category)]++
		}
	}
	m.decLat.observe(float64(d.LatencyMicros) / 1e6)
}

func (m *Metrics) ObserveProxy(api string, status int, added, upstream time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxy[[2]string{api, fmt.Sprint(status)}]++
	m.addLat.observe(added.Seconds())
	if upstream > 0 {
		m.upLat.observe(upstream.Seconds())
	}
}

func (m *Metrics) Observe(name string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hists[name]
	if h == nil {
		h = newHist()
		m.hists[name] = h
	}
	h.observe(d.Seconds())
}

func writeHist(w io.Writer, name, help string, h *hist) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", name, help, name)
	for i, b := range buckets {
		fmt.Fprintf(w, "%s_bucket{le=\"%g\"} %d\n", name, b, h.counts[i])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n%s_sum %g\n%s_count %d\n", name, h.n, name, h.sum, name, h.n)
}

func esc(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(s)
}

func (m *Metrics) Write(w io.Writer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprint(w, "# HELP rungated_decisions_total Decisions by verdict and action kind\n# TYPE rungated_decisions_total counter\n")
	keys := make([][2]string, 0, len(m.decisions))
	for k := range m.decisions {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	for _, k := range keys {
		fmt.Fprintf(w, "rungated_decisions_total{verdict=\"%s\",kind=\"%s\"} %d\n", esc(k[0]), esc(k[1]), m.decisions[k])
	}
	fmt.Fprint(w, "# HELP rungated_findings_total Decisions containing at least one finding of a category\n# TYPE rungated_findings_total counter\n")
	cats := make([]string, 0, len(m.findings))
	for k := range m.findings {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, k := range cats {
		fmt.Fprintf(w, "rungated_findings_total{category=\"%s\"} %d\n", esc(k), m.findings[k])
	}
	fmt.Fprint(w, "# HELP rungated_proxy_requests_total Proxied LLM requests by API and status\n# TYPE rungated_proxy_requests_total counter\n")
	pk := make([][2]string, 0, len(m.proxy))
	for k := range m.proxy {
		pk = append(pk, k)
	}
	sort.Slice(pk, func(i, j int) bool {
		if pk[i][0] != pk[j][0] {
			return pk[i][0] < pk[j][0]
		}
		return pk[i][1] < pk[j][1]
	})
	for _, k := range pk {
		fmt.Fprintf(w, "rungated_proxy_requests_total{api=\"%s\",status=\"%s\"} %d\n", esc(k[0]), esc(k[1]), m.proxy[k])
	}
	writeHist(w, "rungated_decision_latency_seconds", "Decision pipeline latency", m.decLat)
	writeHist(w, "rungated_proxy_added_latency_seconds", "Latency added by Rungated on proxied requests", m.addLat)
	writeHist(w, "rungated_upstream_latency_seconds", "Upstream provider latency until response headers or body", m.upLat)
	names := make([]string, 0, len(m.hists))
	for k := range m.hists {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		writeHist(w, k, "Custom duration histogram", m.hists[k])
	}
	gn := make([]string, 0, len(m.gauges))
	for k := range m.gauges {
		gn = append(gn, k)
	}
	sort.Strings(gn)
	for _, k := range gn {
		fmt.Fprintf(w, "# TYPE %s gauge\n%s %g\n", k, k, m.gauges[k]())
	}
	fmt.Fprintf(w, "# TYPE rungated_uptime_seconds gauge\nrungated_uptime_seconds %g\n", time.Since(m.started).Seconds())
}
