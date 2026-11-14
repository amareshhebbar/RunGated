package store

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

type state struct {
	Agents       map[string]*AgentRecord    `json:"agents"`
	Policies     map[string][]*PolicyRecord `json:"policies"`
	Approvals    map[string]*Approval       `json:"approvals"`
	Pins         map[string]*Pin            `json:"pins"`
	Incidents    map[string]*Incident       `json:"incidents"`
	Containments map[string]*Containment    `json:"containments"`
}

type MemStore struct {
	mu        sync.RWMutex
	st        state
	events    []core.Event
	eventIdx  map[string]int
	maxEvents int
	audit     []*AuditRecord
	maxAudit  int
	auditMark []auditMark
	auditSize int64
	auditN    int
	maxIncid  int
	maxAppr   int
	evSize    int64
	evRotate  int64
	dir       string
	dirty     bool
	evFile    *os.File
	auditFile *os.File
	stop      chan struct{}
	done      chan struct{}
}

func NewMemStore(dir string, maxEvents int) (*MemStore, error) {
	if maxEvents <= 0 {
		maxEvents = 20000
	}
	s := &MemStore{
		st: state{
			Agents: map[string]*AgentRecord{}, Policies: map[string][]*PolicyRecord{}, Approvals: map[string]*Approval{},
			Pins: map[string]*Pin{}, Incidents: map[string]*Incident{}, Containments: map[string]*Containment{},
		},
		eventIdx:  map[string]int{},
		maxEvents: maxEvents,
		maxAudit:  50000,
		maxIncid:  20000,
		maxAppr:   20000,
		evRotate:  256 << 20,
		dir:       dir,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := s.load(); err != nil {
			return nil, err
		}
		var err error
		s.evFile, err = os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		s.auditFile, err = os.OpenFile(filepath.Join(dir, "audit.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, err
		}
		go s.flusher()
	} else {
		close(s.done)
	}
	return s, nil
}

func (s *MemStore) load() error {
	if b, err := os.ReadFile(filepath.Join(s.dir, "state.json")); err == nil {
		if err := json.Unmarshal(b, &s.st); err != nil {
			return err
		}
		if s.st.Agents == nil {
			s.st.Agents = map[string]*AgentRecord{}
		}
		if s.st.Policies == nil {
			s.st.Policies = map[string][]*PolicyRecord{}
		}
		if s.st.Approvals == nil {
			s.st.Approvals = map[string]*Approval{}
		}
		if s.st.Pins == nil {
			s.st.Pins = map[string]*Pin{}
		}
		if s.st.Incidents == nil {
			s.st.Incidents = map[string]*Incident{}
		}
		if s.st.Containments == nil {
			s.st.Containments = map[string]*Containment{}
		}
	}
	if f, err := os.Open(filepath.Join(s.dir, "audit.jsonl")); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			line := sc.Bytes()
			var r AuditRecord
			if json.Unmarshal(line, &r) == nil {
				s.markAudit(r.Seq)
				s.audit = append(s.audit, &r)
				s.trimAudit()
			}
			s.auditSize += int64(len(line)) + 1
		}
		f.Close()
	}
	for _, name := range []string{"events.jsonl.1", "events.jsonl"} {
		s.loadEvents(filepath.Join(s.dir, name), name == "events.jsonl")
	}
	if len(s.events) > s.maxEvents {
		s.events = append([]core.Event{}, s.events[len(s.events)-s.maxEvents:]...)
	}
	s.reindex()
	return nil
}

func (s *MemStore) loadEvents(path string, current bool) {
	if f, err := os.Open(path); err == nil {
		if st, err := f.Stat(); err == nil && current {
			s.evSize = st.Size()
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<26)
		for sc.Scan() {
			var e core.Event
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				s.events = append(s.events, e)
				if len(s.events) > s.maxEvents*2 {
					s.events = append([]core.Event{}, s.events[len(s.events)-s.maxEvents:]...)
				}
			}
		}
		f.Close()
	}
}

func (s *MemStore) rotateEvents() {
	if s.evFile == nil || s.evRotate <= 0 || s.evSize < s.evRotate {
		return
	}
	s.evFile.Sync()
	s.evFile.Close()
	cur := filepath.Join(s.dir, "events.jsonl")
	os.Rename(cur, cur+".1")
	f, err := os.OpenFile(cur, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.evFile = nil
		return
	}
	s.evFile = f
	s.evSize = 0
}

func (s *MemStore) reindex() {
	s.eventIdx = make(map[string]int, len(s.events))
	for i, e := range s.events {
		s.eventIdx[e.ID] = i
	}
}

func (s *MemStore) flusher() {
	defer close(s.done)
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.flushState()
		case <-s.stop:
			s.flushState()
			return
		}
	}
}

func (s *MemStore) flushState() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	b, err := json.Marshal(s.st)
	s.dirty = false
	s.mu.Unlock()
	if err != nil {
		return
	}
	tmp := filepath.Join(s.dir, "state.json.tmp")
	if os.WriteFile(tmp, b, 0o600) == nil {
		os.Rename(tmp, filepath.Join(s.dir, "state.json"))
	}
}

func clone[T any](v *T) *T {
	b, _ := json.Marshal(v)
	var out T
	json.Unmarshal(b, &out)
	return &out
}

func (s *MemStore) PutAgent(_ context.Context, a *AgentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Agents[a.ID] = clone(a)
	s.dirty = true
	return nil
}

func (s *MemStore) GetAgent(_ context.Context, id string) (*AgentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.st.Agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(a), nil
}

func (s *MemStore) ListAgents(_ context.Context, tenant string) ([]*AgentRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*AgentRecord
	for _, a := range s.st.Agents {
		if tenant == "" || a.Tenant == tenant {
			out = append(out, clone(a))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *MemStore) DeleteAgent(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Agents[id]; !ok {
		return ErrNotFound
	}
	delete(s.st.Agents, id)
	s.dirty = true
	return nil
}

func (s *MemStore) PutPolicy(_ context.Context, tenant, src, author string) (*PolicyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.st.Policies[tenant]
	r := &PolicyRecord{Tenant: tenant, Source: src, Version: len(h) + 1, Author: author, UpdatedAt: time.Now().UTC()}
	s.st.Policies[tenant] = append(h, r)
	s.dirty = true
	return clone(r), nil
}

func (s *MemStore) GetPolicy(_ context.Context, tenant string) (*PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := s.st.Policies[tenant]
	if len(h) == 0 {
		return nil, ErrNotFound
	}
	return clone(h[len(h)-1]), nil
}

func (s *MemStore) ListPolicies(_ context.Context) ([]*PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*PolicyRecord
	for _, h := range s.st.Policies {
		if len(h) > 0 {
			out = append(out, clone(h[len(h)-1]))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tenant < out[j].Tenant })
	return out, nil
}

func (s *MemStore) PolicyHistory(_ context.Context, tenant string) ([]*PolicyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*PolicyRecord
	for _, r := range s.st.Policies[tenant] {
		out = append(out, clone(r))
	}
	return out, nil
}

func (s *MemStore) PutApproval(_ context.Context, a *Approval) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Approvals[a.ID] = clone(a)
	s.dirty = true
	if s.maxAppr > 0 && len(s.st.Approvals) > s.maxAppr+s.maxAppr/4 {
		pruneOldest(s.st.Approvals, len(s.st.Approvals)-s.maxAppr, func(x *Approval) (bool, time.Time) { return x.Status == "pending", x.CreatedAt })
	}
	return nil
}

func (s *MemStore) GetApproval(_ context.Context, id string) (*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.st.Approvals[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(a), nil
}

func (s *MemStore) ListApprovals(_ context.Context, f ApprovalFilter) ([]*Approval, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Approval
	for _, a := range s.st.Approvals {
		if (f.Tenant == "" || a.Tenant == f.Tenant) && (f.Status == "" || a.Status == f.Status) && (f.AgentID == "" || a.AgentID == f.AgentID) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	res := make([]*Approval, len(out))
	for i, a := range out {
		res[i] = clone(a)
	}
	return res, nil
}

func (s *MemStore) PutPin(_ context.Context, p *Pin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Pins[p.Key()] = clone(p)
	s.dirty = true
	return nil
}

func (s *MemStore) GetPin(_ context.Context, tenant, server, tool string) (*Pin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.st.Pins[tenant+"/"+server+"/"+tool]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(p), nil
}

func (s *MemStore) ListPins(_ context.Context, tenant string) ([]*Pin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Pin
	for _, p := range s.st.Pins {
		if tenant == "" || p.Tenant == tenant {
			out = append(out, clone(p))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out, nil
}

func (s *MemStore) PutIncident(_ context.Context, i *Incident) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Incidents[i.ID] = clone(i)
	s.dirty = true
	if s.maxIncid > 0 && len(s.st.Incidents) > s.maxIncid+s.maxIncid/4 {
		pruneOldest(s.st.Incidents, len(s.st.Incidents)-s.maxIncid, func(x *Incident) (bool, time.Time) { return x.Status == "open", x.UpdatedAt })
	}
	return nil
}

func pruneOldest[T any](m map[string]*T, n int, info func(*T) (bool, time.Time)) {
	type kv struct {
		id    string
		keep  bool
		stamp time.Time
	}
	all := make([]kv, 0, len(m))
	for id, v := range m {
		keep, t := info(v)
		all = append(all, kv{id, keep, t})
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].keep != all[b].keep {
			return !all[a].keep
		}
		return all[a].stamp.Before(all[b].stamp)
	})
	for i := 0; i < n && i < len(all); i++ {
		delete(m, all[i].id)
	}
}

func (s *MemStore) GetIncident(_ context.Context, id string) (*Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i, ok := s.st.Incidents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(i), nil
}

func (s *MemStore) ListIncidents(_ context.Context, f IncidentFilter) ([]*Incident, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Incident
	for _, i := range s.st.Incidents {
		if (f.Tenant == "" || i.Tenant == f.Tenant) && (f.Status == "" || i.Status == f.Status) && (f.AgentID == "" || i.AgentID == f.AgentID) {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].UpdatedAt.After(out[b].UpdatedAt) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	res := make([]*Incident, len(out))
	for k, i := range out {
		res[k] = clone(i)
	}
	return res, nil
}

func (s *MemStore) PutContainment(_ context.Context, c *Containment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.st.Containments[c.ID] = clone(c)
	s.dirty = true
	return nil
}

func (s *MemStore) ListContainments(_ context.Context, activeOnly bool) ([]*Containment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Containment
	for _, c := range s.st.Containments {
		if !activeOnly || c.ReleasedAt == nil {
			out = append(out, clone(c))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MemStore) AppendEvents(_ context.Context, evs []core.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range evs {
		if i, ok := s.eventIdx[e.ID]; ok {
			s.events[i] = e
		} else {
			s.events = append(s.events, e)
			s.eventIdx[e.ID] = len(s.events) - 1
		}
		if s.evFile != nil {
			if b, err := json.Marshal(e); err == nil {
				s.evFile.Write(append(b, '\n'))
				s.evSize += int64(len(b)) + 1
			}
		}
	}
	if len(s.events) > s.maxEvents+s.maxEvents/4 {
		s.events = append([]core.Event{}, s.events[len(s.events)-s.maxEvents:]...)
		s.reindex()
	}
	s.rotateEvents()
	return nil
}

func verdictRank(v string) int {
	return core.Verdict(v).Rank()
}

func (s *MemStore) QueryEvents(_ context.Context, f EventFilter) ([]core.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 200
	}
	if len(f.IDs) > 0 {
		var out []core.Event
		for _, id := range f.IDs {
			if i, ok := s.eventIdx[id]; ok {
				out = append(out, s.events[i])
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
		return out, nil
	}
	var out []core.Event
	for i := len(s.events) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.events[i]
		if f.Tenant != "" && e.TenantID != f.Tenant {
			continue
		}
		if f.AgentID != "" && e.AgentID != f.AgentID {
			continue
		}
		if f.SessionID != "" && e.SessionID != f.SessionID {
			continue
		}
		if f.Type != "" && e.Type != f.Type {
			continue
		}
		if !f.Since.IsZero() && e.Time.Before(f.Since) {
			continue
		}
		if !f.Before.IsZero() && !e.Time.Before(f.Before) {
			continue
		}
		if f.MinVerdict != "" {
			if e.Decision == nil || e.Decision.Computed.Rank() < verdictRank(f.MinVerdict) && e.Decision.Verdict.Rank() < verdictRank(f.MinVerdict) {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

type auditMark struct {
	seq    uint64
	offset int64
}

func (s *MemStore) markAudit(seq uint64) {
	if s.auditN%1000 == 0 {
		s.auditMark = append(s.auditMark, auditMark{seq: seq, offset: s.auditSize})
	}
	s.auditN++
}

func (s *MemStore) trimAudit() {
	if s.dir == "" || s.maxAudit <= 0 || len(s.audit) <= s.maxAudit+s.maxAudit/4 {
		return
	}
	s.audit = append([]*AuditRecord{}, s.audit[len(s.audit)-s.maxAudit:]...)
}

func (s *MemStore) AppendAudit(_ context.Context, r *AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.auditFile != nil {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		s.markAudit(r.Seq)
		if _, err := s.auditFile.Write(append(b, '\n')); err != nil {
			return err
		}
		s.auditSize += int64(len(b)) + 1
	}
	s.audit = append(s.audit, clone(r))
	s.trimAudit()
	return nil
}

func (s *MemStore) auditFromDisk(fromSeq uint64, limit int) ([]*AuditRecord, error) {
	var off int64
	for _, m := range s.auditMark {
		if m.seq > fromSeq {
			break
		}
		off = m.offset
	}
	f, err := os.Open(filepath.Join(s.dir, "audit.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, 0); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	var out []*AuditRecord
	for sc.Scan() && len(out) < limit {
		var r AuditRecord
		if json.Unmarshal(sc.Bytes(), &r) != nil || r.Seq < fromSeq {
			continue
		}
		out = append(out, &r)
	}
	return out, sc.Err()
}

func (s *MemStore) LastAudit(_ context.Context) (*AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.audit) == 0 {
		return nil, ErrNotFound
	}
	return clone(s.audit[len(s.audit)-1]), nil
}

func (s *MemStore) ListAudit(_ context.Context, fromSeq uint64, limit int) ([]*AuditRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 1000
	}
	if s.dir != "" && len(s.audit) > 0 && fromSeq < s.audit[0].Seq {
		return s.auditFromDisk(fromSeq, limit)
	}
	i := sort.Search(len(s.audit), func(i int) bool { return s.audit[i].Seq >= fromSeq })
	var out []*AuditRecord
	for ; i < len(s.audit) && len(out) < limit; i++ {
		out = append(out, clone(s.audit[i]))
	}
	return out, nil
}

func (s *MemStore) TamperAuditForTest(seq uint64, verdict string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.audit {
		if r.Seq == seq {
			r.Verdict = verdict
		}
	}
}

func (s *MemStore) Ping(context.Context) error { return nil }

func (s *MemStore) Close() error {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
	if s.evFile != nil {
		s.evFile.Sync()
		s.evFile.Close()
	}
	if s.auditFile != nil {
		s.auditFile.Sync()
		s.auditFile.Close()
	}
	return nil
}
