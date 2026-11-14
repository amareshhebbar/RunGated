package approvals

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
	StatusExpired  = "expired"
)

type Result struct {
	Status  string `json:"status"`
	Allowed bool   `json:"allowed"`
	By      string `json:"by,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type Notifier interface {
	Name() string
	Notify(ctx context.Context, a *store.Approval) error
	Resolved(ctx context.Context, a *store.Approval) error
}

type Service struct {
	st        store.Store
	emit      func(core.Event)
	notifiers []Notifier
	mu        sync.Mutex
	waiters   map[string][]chan Result
	hooks     map[string][]func(*store.Approval)
	stop      chan struct{}
}

func New(st store.Store, emit func(core.Event)) *Service {
	return &Service{st: st, emit: emit, waiters: map[string][]chan Result{}, hooks: map[string][]func(*store.Approval){}, stop: make(chan struct{})}
}

func (s *Service) AddNotifier(n Notifier) { s.notifiers = append(s.notifiers, n) }

func (s *Service) OnResolve(kind string, fn func(*store.Approval)) {
	s.mu.Lock()
	s.hooks[kind] = append(s.hooks[kind], fn)
	s.mu.Unlock()
}

func (s *Service) Create(ctx context.Context, a *store.Approval) error {
	if a.ID == "" {
		a.ID = core.NewID("apr")
	}
	a.Status = StatusPending
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.OnTimeout == "" {
		a.OnTimeout = "deny"
	}
	if err := s.st.PutApproval(ctx, a); err != nil {
		return err
	}
	if s.emit != nil {
		s.emit(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: a.Tenant, AgentID: a.AgentID, SessionID: a.SessionID, Type: "approval.requested", Message: a.Summary, Data: map[string]any{"approval_id": a.ID, "kind": a.Kind, "expires_at": a.ExpiresAt}})
	}
	for _, n := range s.notifiers {
		go func(n Notifier) {
			nctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := n.Notify(nctx, a); err != nil {
				slog.Warn("approval notify failed", "notifier", n.Name(), "err", err)
			}
		}(n)
	}
	return nil
}

func (s *Service) Wait(ctx context.Context, id string) Result {
	a, err := s.st.GetApproval(ctx, id)
	if err != nil {
		return Result{Status: StatusDenied, Reason: "approval not found"}
	}
	if a.Status != StatusPending {
		return resultOf(a)
	}
	ch := make(chan Result, 1)
	s.mu.Lock()
	s.waiters[id] = append(s.waiters[id], ch)
	s.mu.Unlock()
	a, err = s.st.GetApproval(ctx, id)
	if err == nil && a.Status != StatusPending {
		return resultOf(a)
	}
	timer := time.NewTimer(time.Until(a.ExpiresAt) + 250*time.Millisecond)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r
	case <-timer.C:
		s.expire(context.Background(), id)
		select {
		case r := <-ch:
			return r
		default:
		}
		if a2, err := s.st.GetApproval(context.Background(), id); err == nil {
			return resultOf(a2)
		}
		return Result{Status: StatusExpired, Allowed: a.OnTimeout == "allow"}
	case <-ctx.Done():
		return Result{Status: StatusDenied, Reason: "caller went away"}
	}
}

func resultOf(a *store.Approval) Result {
	r := Result{Status: a.Status, By: a.DecidedBy, Reason: a.Reason}
	switch a.Status {
	case StatusApproved:
		r.Allowed = true
	case StatusExpired:
		r.Allowed = a.OnTimeout == "allow"
	}
	return r
}

var ErrNotPending = errors.New("approval is not pending")

func (s *Service) Decide(ctx context.Context, id string, approve bool, by, reason string) (*store.Approval, error) {
	s.mu.Lock()
	a, err := s.st.GetApproval(ctx, id)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if a.Status != StatusPending {
		s.mu.Unlock()
		return a, ErrNotPending
	}
	now := time.Now().UTC()
	a.DecidedAt = &now
	a.DecidedBy = by
	a.Reason = reason
	if approve {
		a.Status = StatusApproved
	} else {
		a.Status = StatusDenied
	}
	if err := s.st.PutApproval(ctx, a); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	s.resolve(a)
	return a, nil
}

func (s *Service) expire(ctx context.Context, id string) {
	s.mu.Lock()
	a, err := s.st.GetApproval(ctx, id)
	if err != nil || a.Status != StatusPending {
		s.mu.Unlock()
		return
	}
	now := time.Now().UTC()
	a.Status = StatusExpired
	a.DecidedAt = &now
	a.DecidedBy = "timeout"
	a.Reason = "no decision before timeout; on_timeout=" + a.OnTimeout
	s.st.PutApproval(ctx, a)
	s.mu.Unlock()
	s.resolve(a)
}

func (s *Service) resolve(a *store.Approval) {
	r := resultOf(a)
	s.mu.Lock()
	ws := s.waiters[a.ID]
	delete(s.waiters, a.ID)
	hooks := append([]func(*store.Approval){}, s.hooks[a.Kind]...)
	hooks = append(hooks, s.hooks["*"]...)
	s.mu.Unlock()
	for _, w := range ws {
		w <- r
	}
	for _, h := range hooks {
		h(a)
	}
	if s.emit != nil {
		s.emit(core.Event{ID: core.NewID("evt"), Time: time.Now().UTC(), TenantID: a.Tenant, AgentID: a.AgentID, SessionID: a.SessionID, Type: "approval.resolved", Message: a.Status + " by " + a.DecidedBy, Data: map[string]any{"approval_id": a.ID, "status": a.Status, "by": a.DecidedBy, "reason": a.Reason, "kind": a.Kind}})
	}
	for _, n := range s.notifiers {
		go func(n Notifier) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			n.Resolved(ctx, a)
		}(n)
	}
}

func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			list, err := s.st.ListApprovals(ctx, store.ApprovalFilter{Status: StatusPending, Limit: 5000})
			if err != nil {
				continue
			}
			now := time.Now()
			for _, a := range list {
				if now.After(a.ExpiresAt) {
					s.expire(ctx, a.ID)
				}
			}
		}
	}
}
