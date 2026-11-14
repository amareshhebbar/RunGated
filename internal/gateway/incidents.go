package gateway

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

type Incidents struct {
	mu     sync.Mutex
	st     store.Store
	window time.Duration
	open   map[string]string
	seen   map[string]time.Time
}

const maxIncidentEvents = 1000

func NewIncidents(st store.Store) *Incidents {
	return &Incidents{st: st, window: 30 * time.Minute, open: map[string]string{}, seen: map[string]time.Time{}}
}

func titleFor(a *core.Action, d *core.Decision) string {
	cat := ""
	best := 0.0
	for _, f := range d.Findings {
		if v := f.Severity * f.Confidence; v > best {
			best = v
			cat = string(f.Category)
		}
	}
	target := string(a.Kind)
	switch {
	case a.Tool != "":
		target = "tool " + a.Tool
	case a.Host != "":
		target = "egress to " + a.Host
	case a.Model != "":
		target = "model call " + a.Model
	}
	if cat == "" {
		cat = "risk"
	}
	return fmt.Sprintf("%s on %s (%s)", cat, target, d.Computed)
}

func (in *Incidents) Record(ctx context.Context, e core.Event) string {
	d := e.Decision
	a := e.Action
	if d == nil || a == nil || d.Computed.Rank() < core.VerdictHold.Rank() {
		return ""
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	key := e.TenantID + "/" + e.AgentID + "/" + e.SessionID
	now := e.Time
	if id, ok := in.open[key]; ok {
		inc, err := in.st.GetIncident(ctx, id)
		if err == nil && inc.Status == "open" && now.Sub(inc.UpdatedAt) < in.window {
			inc.EventIDs = capIDs(append(inc.EventIDs, e.ID))
			in.seen[key] = now
			if d.Score > inc.Severity {
				inc.Severity = d.Score
				inc.Title = titleFor(a, d)
			}
			if d.Computed.Rank() > core.Verdict(inc.Verdict).Rank() {
				inc.Verdict = string(d.Computed)
			}
			inc.Categories = mergeCats(inc.Categories, d.Findings)
			inc.UpdatedAt = now
			in.st.PutIncident(ctx, inc)
			return inc.ID
		}
	}
	inc := &store.Incident{
		ID: core.NewID("inc"), Tenant: e.TenantID, AgentID: e.AgentID, SessionID: e.SessionID,
		Title: titleFor(a, d), Severity: d.Score, Verdict: string(d.Computed), Status: "open",
		Categories: mergeCats(nil, d.Findings), EventIDs: []string{e.ID}, CreatedAt: now, UpdatedAt: now,
	}
	in.st.PutIncident(ctx, inc)
	in.open[key] = inc.ID
	in.seen[key] = now
	return inc.ID
}

func capIDs(ids []string) []string {
	if len(ids) <= maxIncidentEvents {
		return ids
	}
	return append([]string{}, ids[len(ids)-maxIncidentEvents:]...)
}

func (in *Incidents) GC(now time.Time) int {
	in.mu.Lock()
	defer in.mu.Unlock()
	n := 0
	for k, t := range in.seen {
		if now.Sub(t) > in.window {
			delete(in.seen, k)
			delete(in.open, k)
			n++
		}
	}
	for k := range in.open {
		if _, ok := in.seen[k]; !ok {
			delete(in.open, k)
			n++
		}
	}
	return n
}

func (in *Incidents) Attach(ctx context.Context, incidentID, eventID, note, snapshot string) {
	if incidentID == "" {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	inc, err := in.st.GetIncident(ctx, incidentID)
	if err != nil {
		return
	}
	if eventID != "" {
		inc.EventIDs = capIDs(append(inc.EventIDs, eventID))
	}
	if note != "" {
		inc.Notes = append(inc.Notes, time.Now().UTC().Format(time.RFC3339)+" "+note)
	}
	if snapshot != "" {
		inc.SnapshotRef = snapshot
	}
	inc.UpdatedAt = time.Now().UTC()
	in.st.PutIncident(ctx, inc)
}

func mergeCats(have []string, fs []core.Finding) []string {
	set := map[string]bool{}
	for _, c := range have {
		set[c] = true
	}
	for _, f := range fs {
		set[string(f.Category)] = true
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
