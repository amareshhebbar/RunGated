package contain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

type SnapshotStore interface {
	Put(ctx context.Context, key string, data []byte) (string, error)
}

type Service struct {
	st        store.Store
	issuer    *Issuer
	snapshots SnapshotStore
	emit      func(core.Event)
	onKill    []func(tenant, agent, session string)
	mu        sync.RWMutex
	agents    map[string]*store.Containment
	sessions  map[string]*store.Containment
}

func New(ctx context.Context, st store.Store, issuer *Issuer, snaps SnapshotStore, emit func(core.Event)) (*Service, error) {
	s := &Service{st: st, issuer: issuer, snapshots: snaps, emit: emit, agents: map[string]*store.Containment{}, sessions: map[string]*store.Containment{}}
	list, err := st.ListContainments(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		s.index(c)
	}
	return s, nil
}

func (s *Service) Issuer() *Issuer { return s.issuer }

func (s *Service) OnKill(fn func(tenant, agent, session string)) { s.onKill = append(s.onKill, fn) }

func (s *Service) index(c *store.Containment) {
	if c.SessionID != "" {
		s.sessions[c.AgentID+"/"+c.SessionID] = c
	} else {
		s.agents[c.AgentID] = c
	}
}

func (s *Service) IsContained(agent, session string) (*store.Containment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if c, ok := s.agents[agent]; ok {
		return c, true
	}
	if session != "" {
		if c, ok := s.sessions[agent+"/"+session]; ok {
			return c, true
		}
	}
	return nil, false
}

func (s *Service) TokenRevoked(c *Claims) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ct, ok := s.agents[c.Agent]; ok && c.Issued <= ct.RevokedBefore.UnixMilli() {
		return true
	}
	if c.Session != "" {
		if ct, ok := s.sessions[c.Agent+"/"+c.Session]; ok && c.Issued <= ct.RevokedBefore.UnixMilli() {
			return true
		}
	}
	return false
}

type Snapshot struct {
	Containment *store.Containment `json:"containment"`
	Trigger     *core.Event        `json:"trigger,omitempty"`
	Recent      []core.Event       `json:"recent_events"`
	TakenAt     time.Time          `json:"taken_at"`
}

func (s *Service) Contain(ctx context.Context, tenant, agent, session, reason, by string, automatic bool, trigger *core.Event) (*store.Containment, error) {
	now := time.Now().UTC()
	c := &store.Containment{ID: core.NewID("ctn"), Tenant: tenant, AgentID: agent, SessionID: session, Reason: reason, By: by, Automatic: automatic, CreatedAt: now, RevokedBefore: now}
	if s.snapshots != nil {
		recent, _ := s.st.QueryEvents(ctx, store.EventFilter{AgentID: agent, SessionID: session, Limit: 200})
		snap := Snapshot{Containment: c, Trigger: trigger, Recent: recent, TakenAt: now}
		b, _ := json.MarshalIndent(snap, "", "  ")
		ref, err := s.snapshots.Put(ctx, fmt.Sprintf("%s/%s/%s.json", tenant, agent, c.ID), b)
		if err != nil {
			slog.Error("forensic snapshot failed", "err", err)
		} else {
			c.SnapshotRef = ref
		}
	}
	if err := s.st.PutContainment(ctx, c); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.index(c)
	s.mu.Unlock()
	if rec, err := s.st.GetAgent(ctx, agent); err == nil && session == "" {
		rec.Status = "contained"
		rec.UpdatedAt = now
		s.st.PutAgent(ctx, rec)
	}
	for _, fn := range s.onKill {
		fn(tenant, agent, session)
	}
	if s.emit != nil {
		scope := "agent"
		if session != "" {
			scope = "session"
		}
		s.emit(core.Event{ID: core.NewID("evt"), Time: now, TenantID: tenant, AgentID: agent, SessionID: session, Type: "containment.applied", Message: reason, Data: map[string]any{"containment_id": c.ID, "scope": scope, "by": by, "automatic": automatic, "snapshot": c.SnapshotRef}})
	}
	return c, nil
}

func (s *Service) Release(ctx context.Context, id, by string) (*store.Containment, error) {
	list, err := s.st.ListContainments(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		if c.ID != id && !(id == c.AgentID && c.SessionID == "") {
			continue
		}
		now := time.Now().UTC()
		c.ReleasedAt = &now
		if err := s.st.PutContainment(ctx, c); err != nil {
			return nil, err
		}
		s.mu.Lock()
		if c.SessionID != "" {
			delete(s.sessions, c.AgentID+"/"+c.SessionID)
		} else {
			delete(s.agents, c.AgentID)
		}
		s.mu.Unlock()
		if rec, err := s.st.GetAgent(ctx, c.AgentID); err == nil && c.SessionID == "" {
			rec.Status = "active"
			rec.UpdatedAt = now
			s.st.PutAgent(ctx, rec)
		}
		if s.emit != nil {
			s.emit(core.Event{ID: core.NewID("evt"), Time: now, TenantID: c.Tenant, AgentID: c.AgentID, SessionID: c.SessionID, Type: "containment.released", Message: "released by " + by, Data: map[string]any{"containment_id": c.ID, "by": by}})
		}
		return c, nil
	}
	return nil, store.ErrNotFound
}

func (s *Service) Active() []*store.Containment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*store.Containment
	for _, c := range s.agents {
		out = append(out, c)
	}
	for _, c := range s.sessions {
		out = append(out, c)
	}
	return out
}
