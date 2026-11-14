package detect

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) take(rate float64, burst float64, now time.Time, n float64) bool {
	if b.last.IsZero() {
		b.tokens = burst
		b.last = now
	}
	b.tokens += now.Sub(b.last).Seconds() * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	b.last = now
	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}

type window struct {
	start time.Time
	sum   float64
}

func (w *window) current(now time.Time, span time.Duration) float64 {
	if w.start.IsZero() || now.Sub(w.start) >= span {
		return 0
	}
	return w.sum
}

func (w *window) add(now time.Time, span time.Duration, v float64) float64 {
	if w.start.IsZero() || now.Sub(w.start) >= span {
		w.start = now
		w.sum = 0
	}
	w.sum += v
	return w.sum
}

type loopState struct {
	hash  string
	count int
	last  time.Time
}

type agentState struct {
	requests   bucket
	toolCalls  bucket
	sessReq    map[string]*bucket
	tokensHour window
	costDay    window
	egressHour window
	loops      map[string]*loopState
	lastSeen   time.Time
}

type Limiter struct {
	mu     sync.Mutex
	agents map[string]*agentState
	now    func() time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{agents: map[string]*agentState{}, now: time.Now}
}

func (l *Limiter) state(agent string) *agentState {
	s := l.agents[agent]
	if s == nil {
		s = &agentState{sessReq: map[string]*bucket{}, loops: map[string]*loopState{}}
		l.agents[agent] = s
	}
	return s
}

func ArgsHash(tool string, args map[string]any) string {
	b, _ := json.Marshal(args)
	h := sha256.Sum256(append([]byte(tool+"\x00"), b...))
	return hex.EncodeToString(h[:8])
}

func (l *Limiter) Check(a *core.Action, m *manifest.Manifest) []core.Finding {
	lim := m.Spec.Limits
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state(a.AgentID)
	s.lastSeen = now
	var out []core.Finding
	hard := func(cat core.Category, rule, msg string, sev float64) {
		f := core.Finding{Detector: "limits", Tier: 0, Category: cat, Rule: rule, Severity: sev, Confidence: 1, Message: msg}
		if sev >= 0.8 {
			f.Min = core.VerdictBlock
		} else {
			f.Min = core.VerdictHold
		}
		out = append(out, f)
	}
	switch a.Kind {
	case core.KindLLMRequest, core.KindEgressRequest:
		if lim.RequestsPerMinute > 0 {
			rate := float64(lim.RequestsPerMinute) / 60
			if !s.requests.take(rate, float64(lim.RequestsPerMinute), now, 1) {
				hard(core.CatRate, "requests_per_minute", fmt.Sprintf("agent exceeded %d requests per minute", lim.RequestsPerMinute), 0.85)
			}
			if a.SessionID != "" {
				b := s.sessReq[a.SessionID]
				if b == nil {
					b = &bucket{}
					s.sessReq[a.SessionID] = b
				}
				if !b.take(rate, float64(lim.RequestsPerMinute), now, 1) {
					hard(core.CatRate, "session_requests_per_minute", "session exceeded request rate", 0.85)
				}
			}
		}
	case core.KindToolCall:
		if a.Meta["proposed"] == "1" {
			break
		}
		if lim.ToolCallsPerMinute > 0 {
			if !s.toolCalls.take(float64(lim.ToolCallsPerMinute)/60, float64(lim.ToolCallsPerMinute), now, 1) {
				hard(core.CatRate, "tool_calls_per_minute", fmt.Sprintf("agent exceeded %d tool calls per minute", lim.ToolCallsPerMinute), 0.85)
			}
		}
		if lim.MaxLoopRepeats > 0 {
			key := a.SessionID
			h := ArgsHash(a.Server+"/"+a.Tool, a.Args)
			ls := s.loops[key]
			if ls == nil {
				ls = &loopState{}
				s.loops[key] = ls
			}
			if ls.hash == h && now.Sub(ls.last) < 10*time.Minute {
				ls.count++
			} else {
				ls.hash = h
				ls.count = 1
			}
			ls.last = now
			if ls.count > lim.MaxLoopRepeats {
				sev := 0.7
				if ls.count > lim.MaxLoopRepeats*2 {
					sev = 0.9
				}
				hard(core.CatLoop, "identical_tool_call_loop", fmt.Sprintf("identical tool call repeated %d times", ls.count), sev)
			}
		}
	}
	if a.Kind == core.KindLLMRequest {
		if lim.TokensPerHour > 0 {
			if v := s.tokensHour.current(now, time.Hour); v > float64(lim.TokensPerHour) {
				hard(core.CatCost, "tokens_per_hour", fmt.Sprintf("token budget exceeded: %.0f of %d per hour", v, lim.TokensPerHour), 0.85)
			}
		}
		if lim.CostPerDayUSD > 0 {
			if v := s.costDay.current(now, 24*time.Hour); v > lim.CostPerDayUSD {
				hard(core.CatCost, "cost_per_day", fmt.Sprintf("cost budget exceeded: $%.2f of $%.2f per day", v, lim.CostPerDayUSD), 0.9)
			}
		}
	}
	if lim.EgressBytesPerHour > 0 && a.Kind == core.KindEgressRequest && a.Bytes > 0 {
		if v := s.egressHour.add(now, time.Hour, float64(a.Bytes)); v > float64(lim.EgressBytesPerHour) {
			hard(core.CatExfil, "egress_bytes_per_hour", "egress volume budget exceeded", 0.85)
		}
	}
	return out
}

func (l *Limiter) Usage(agent string, tokens int, cost float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.state(agent)
	now := l.now()
	if tokens > 0 {
		s.tokensHour.add(now, time.Hour, float64(tokens))
	}
	if cost > 0 {
		s.costDay.add(now, 24*time.Hour, cost)
	}
}

func (l *Limiter) Snapshot(agent string) map[string]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	s := l.agents[agent]
	if s == nil {
		return map[string]float64{}
	}
	return map[string]float64{"tokens_hour": s.tokensHour.sum, "cost_day_usd": s.costDay.sum, "egress_bytes_hour": s.egressHour.sum}
}

func (l *Limiter) GC(maxIdle time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, s := range l.agents {
		if now.Sub(s.lastSeen) > maxIdle {
			delete(l.agents, k)
			continue
		}
		for sk, ls := range s.loops {
			if now.Sub(ls.last) > maxIdle {
				delete(s.loops, sk)
			}
		}
		if len(s.sessReq) > 10000 {
			s.sessReq = map[string]*bucket{}
		}
	}
}
