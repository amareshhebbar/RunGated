package gateway

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/contain"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/policy"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

type Agent struct {
	Manifest *manifest.Manifest
	Record   *store.AgentRecord
}

type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	policy *policy.Engine
	st     store.Store
	seen   map[string]time.Time
}

func NewRegistry(st store.Store, pe *policy.Engine) *Registry {
	return &Registry{agents: map[string]*Agent{}, policy: pe, st: st, seen: map[string]time.Time{}}
}

func (r *Registry) LoadAll(ctx context.Context) error {
	pols, err := r.st.ListPolicies(ctx)
	if err != nil {
		return err
	}
	for _, p := range pols {
		if err := r.policy.SetCustom(p.Tenant, p.Source); err != nil {
			return fmt.Errorf("tenant %s policy: %w", p.Tenant, err)
		}
	}
	recs, err := r.st.ListAgents(ctx, "")
	if err != nil {
		return err
	}
	for _, rec := range recs {
		m, err := manifest.Parse([]byte(rec.Manifest))
		if err != nil {
			return fmt.Errorf("agent %s: %w", rec.ID, err)
		}
		if err := r.policy.Load(m); err != nil {
			return fmt.Errorf("agent %s: %w", rec.ID, err)
		}
		r.agents[rec.ID] = &Agent{Manifest: m, Record: rec}
	}
	return nil
}

type RegisterResult struct {
	Agent   *Agent
	Key     string
	Created bool
}

func (r *Registry) Register(ctx context.Context, raw []byte, rotateKey bool) (*RegisterResult, error) {
	m, err := manifest.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := r.policy.Load(m); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	res := &RegisterResult{}
	prev, err := r.st.GetAgent(ctx, m.Metadata.ID)
	rec := &store.AgentRecord{ID: m.Metadata.ID, Tenant: m.Metadata.Tenant, Manifest: string(raw), Status: "active", CreatedAt: now, UpdatedAt: now, Version: 1}
	if err == nil {
		if prev.Tenant != m.Metadata.Tenant {
			return nil, errors.New("agent id already registered under another tenant")
		}
		rec.CreatedAt = prev.CreatedAt
		rec.Version = prev.Version + 1
		rec.ModeOverride = prev.ModeOverride
		rec.Status = prev.Status
		rec.KeyHash = prev.KeyHash
		rec.LastSeen = prev.LastSeen
	} else {
		res.Created = true
	}
	if rec.KeyHash == "" || rotateKey {
		res.Key = contain.NewAgentKey()
		rec.KeyHash = contain.HashKey(res.Key)
	}
	if err := r.st.PutAgent(ctx, rec); err != nil {
		return nil, err
	}
	a := &Agent{Manifest: m, Record: rec}
	r.agents[m.Metadata.ID] = a
	res.Agent = a
	return res, nil
}

func (r *Registry) Get(id string) *Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agents[id]
}

func (r *Registry) List(tenant string) []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Agent
	for _, a := range r.agents {
		if tenant == "" || a.Manifest.Metadata.Tenant == tenant {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manifest.Metadata.ID < out[j].Manifest.Metadata.ID })
	return out
}

func (r *Registry) SetMode(ctx context.Context, id string, mode core.Mode) error {
	if mode != "" && !core.ValidMode(mode) {
		return fmt.Errorf("invalid mode %q", mode)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.agents[id]
	if a == nil {
		return store.ErrNotFound
	}
	rec := *a.Record
	rec.ModeOverride = string(mode)
	rec.UpdatedAt = time.Now().UTC()
	if err := r.st.PutAgent(ctx, &rec); err != nil {
		return err
	}
	a.Record = &rec
	return nil
}

func (r *Registry) SetStatus(id, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.agents[id]; a != nil {
		rec := *a.Record
		rec.Status = status
		a.Record = &rec
	}
}

func (r *Registry) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.st.DeleteAgent(ctx, id); err != nil {
		return err
	}
	delete(r.agents, id)
	r.policy.Remove(id)
	return nil
}

func (r *Registry) Touch(id string) {
	r.mu.Lock()
	r.seen[id] = time.Now().UTC()
	r.mu.Unlock()
}

func (r *Registry) LastSeen(id string) time.Time {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.seen[id]
}

func (r *Registry) CheckKey(id, key string) bool {
	a := r.Get(id)
	if a == nil || a.Record.KeyHash == "" {
		return false
	}
	return a.Record.KeyHash == contain.HashKey(key)
}

func (a *Agent) Mode(kind core.Kind, tool string) core.Mode {
	if a.Record != nil && a.Record.ModeOverride != "" {
		return core.Mode(a.Record.ModeOverride)
	}
	return core.Mode(a.Manifest.ModeFor(string(kind), tool))
}
