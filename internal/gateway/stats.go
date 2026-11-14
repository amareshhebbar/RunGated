package gateway

import (
	"sort"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

type Stats struct {
	mu       sync.Mutex
	lat      []int64
	pos      int
	full     bool
	verdicts map[core.Verdict]uint64
	computed map[core.Verdict]uint64
	kinds    map[core.Kind]uint64
	cats     map[core.Category]uint64
	total    uint64
	degraded uint64
	started  time.Time
}

func NewStats() *Stats {
	return &Stats{lat: make([]int64, 20000), verdicts: map[core.Verdict]uint64{}, computed: map[core.Verdict]uint64{}, kinds: map[core.Kind]uint64{}, cats: map[core.Category]uint64{}, started: time.Now()}
}

func (s *Stats) Record(a *core.Action, d *core.Decision) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lat[s.pos] = d.LatencyMicros
	s.pos++
	if s.pos == len(s.lat) {
		s.pos = 0
		s.full = true
	}
	s.total++
	s.verdicts[d.Verdict]++
	s.computed[d.Computed]++
	s.kinds[a.Kind]++
	if d.Degraded {
		s.degraded++
	}
	seen := map[core.Category]bool{}
	for _, f := range d.Findings {
		if !seen[f.Category] {
			s.cats[f.Category]++
			seen[f.Category] = true
		}
	}
}

type Snapshot struct {
	Total      uint64                   `json:"total"`
	Verdicts   map[core.Verdict]uint64  `json:"verdicts"`
	Computed   map[core.Verdict]uint64  `json:"computed"`
	Kinds      map[core.Kind]uint64     `json:"kinds"`
	Categories map[core.Category]uint64 `json:"categories"`
	Degraded   uint64                   `json:"degraded"`
	P50Micros  int64                    `json:"p50_us"`
	P95Micros  int64                    `json:"p95_us"`
	P99Micros  int64                    `json:"p99_us"`
	MaxMicros  int64                    `json:"max_us"`
	Samples    int                      `json:"samples"`
	UptimeSec  int64                    `json:"uptime_s"`
}

func (s *Stats) Snapshot() Snapshot {
	s.mu.Lock()
	n := s.pos
	if s.full {
		n = len(s.lat)
	}
	cp := append([]int64{}, s.lat[:n]...)
	out := Snapshot{Total: s.total, Verdicts: map[core.Verdict]uint64{}, Computed: map[core.Verdict]uint64{}, Kinds: map[core.Kind]uint64{}, Categories: map[core.Category]uint64{}, Degraded: s.degraded, Samples: n, UptimeSec: int64(time.Since(s.started).Seconds())}
	for k, v := range s.verdicts {
		out.Verdicts[k] = v
	}
	for k, v := range s.computed {
		out.Computed[k] = v
	}
	for k, v := range s.kinds {
		out.Kinds[k] = v
	}
	for k, v := range s.cats {
		out.Categories[k] = v
	}
	s.mu.Unlock()
	if len(cp) > 0 {
		sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
		q := func(p float64) int64 { return cp[int(float64(len(cp)-1)*p)] }
		out.P50Micros, out.P95Micros, out.P99Micros, out.MaxMicros = q(0.5), q(0.95), q(0.99), cp[len(cp)-1]
	}
	return out
}
