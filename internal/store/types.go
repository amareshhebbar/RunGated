package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

var ErrNotFound = errors.New("not found")

type AgentRecord struct {
	ID           string    `json:"id"`
	Tenant       string    `json:"tenant"`
	Manifest     string    `json:"manifest"`
	ModeOverride string    `json:"mode_override,omitempty"`
	Status       string    `json:"status"`
	KeyHash      string    `json:"key_hash,omitempty"`
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastSeen     time.Time `json:"last_seen,omitempty"`
}

type PolicyRecord struct {
	Tenant    string    `json:"tenant"`
	Source    string    `json:"source"`
	Version   int       `json:"version"`
	Author    string    `json:"author"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Approval struct {
	ID        string          `json:"id"`
	Tenant    string          `json:"tenant"`
	AgentID   string          `json:"agent_id"`
	SessionID string          `json:"session_id"`
	Kind      string          `json:"kind"`
	Summary   string          `json:"summary"`
	Action    *core.Action    `json:"action,omitempty"`
	Decision  *core.Decision  `json:"decision,omitempty"`
	Status    string          `json:"status"`
	Reason    string          `json:"reason,omitempty"`
	DecidedBy string          `json:"decided_by,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	DecidedAt *time.Time      `json:"decided_at,omitempty"`
	OnTimeout string          `json:"on_timeout"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

type Pin struct {
	Tenant             string          `json:"tenant"`
	Server             string          `json:"server"`
	Tool               string          `json:"tool"`
	Fingerprint        string          `json:"fingerprint"`
	Description        string          `json:"description"`
	Schema             json.RawMessage `json:"schema,omitempty"`
	Status             string          `json:"status"`
	FirstSeen          time.Time       `json:"first_seen"`
	ApprovedBy         string          `json:"approved_by,omitempty"`
	PendingFingerprint string          `json:"pending_fingerprint,omitempty"`
	PendingDescription string          `json:"pending_description,omitempty"`
	PendingSchema      json.RawMessage `json:"pending_schema,omitempty"`
	PendingSince       *time.Time      `json:"pending_since,omitempty"`
	ApprovalID         string          `json:"approval_id,omitempty"`
}

func (p *Pin) Key() string { return p.Tenant + "/" + p.Server + "/" + p.Tool }

type Incident struct {
	ID          string    `json:"id"`
	Tenant      string    `json:"tenant"`
	AgentID     string    `json:"agent_id"`
	SessionID   string    `json:"session_id"`
	Title       string    `json:"title"`
	Severity    int       `json:"severity"`
	Verdict     string    `json:"verdict"`
	Status      string    `json:"status"`
	Categories  []string  `json:"categories"`
	EventIDs    []string  `json:"event_ids"`
	SnapshotRef string    `json:"snapshot_ref,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Notes       []string  `json:"notes,omitempty"`
}

type Containment struct {
	ID            string     `json:"id"`
	Tenant        string     `json:"tenant"`
	AgentID       string     `json:"agent_id"`
	SessionID     string     `json:"session_id,omitempty"`
	Reason        string     `json:"reason"`
	By            string     `json:"by"`
	Automatic     bool       `json:"automatic"`
	CreatedAt     time.Time  `json:"created_at"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
	SnapshotRef   string     `json:"snapshot_ref,omitempty"`
	RevokedBefore time.Time  `json:"revoked_before"`
}

type AuditRecord struct {
	Seq         uint64    `json:"seq"`
	Time        time.Time `json:"time"`
	EventID     string    `json:"event_id"`
	Tenant      string    `json:"tenant"`
	AgentID     string    `json:"agent_id"`
	Type        string    `json:"type"`
	Verdict     string    `json:"verdict,omitempty"`
	Score       int       `json:"score"`
	EventDigest string    `json:"event_digest"`
	PrevHash    string    `json:"prev_hash"`
	Hash        string    `json:"hash"`
}

type EventFilter struct {
	Tenant     string
	AgentID    string
	SessionID  string
	MinVerdict string
	Type       string
	Since      time.Time
	Before     time.Time
	IDs        []string
	Limit      int
}

type ApprovalFilter struct {
	Tenant  string
	Status  string
	AgentID string
	Limit   int
}

type IncidentFilter struct {
	Tenant  string
	Status  string
	AgentID string
	Limit   int
}

type Store interface {
	PutAgent(ctx context.Context, a *AgentRecord) error
	GetAgent(ctx context.Context, id string) (*AgentRecord, error)
	ListAgents(ctx context.Context, tenant string) ([]*AgentRecord, error)
	DeleteAgent(ctx context.Context, id string) error

	PutPolicy(ctx context.Context, tenant, src, author string) (*PolicyRecord, error)
	GetPolicy(ctx context.Context, tenant string) (*PolicyRecord, error)
	ListPolicies(ctx context.Context) ([]*PolicyRecord, error)
	PolicyHistory(ctx context.Context, tenant string) ([]*PolicyRecord, error)

	PutApproval(ctx context.Context, a *Approval) error
	GetApproval(ctx context.Context, id string) (*Approval, error)
	ListApprovals(ctx context.Context, f ApprovalFilter) ([]*Approval, error)

	PutPin(ctx context.Context, p *Pin) error
	GetPin(ctx context.Context, tenant, server, tool string) (*Pin, error)
	ListPins(ctx context.Context, tenant string) ([]*Pin, error)

	PutIncident(ctx context.Context, i *Incident) error
	GetIncident(ctx context.Context, id string) (*Incident, error)
	ListIncidents(ctx context.Context, f IncidentFilter) ([]*Incident, error)

	PutContainment(ctx context.Context, c *Containment) error
	ListContainments(ctx context.Context, activeOnly bool) ([]*Containment, error)

	AppendEvents(ctx context.Context, evs []core.Event) error
	QueryEvents(ctx context.Context, f EventFilter) ([]core.Event, error)

	AppendAudit(ctx context.Context, r *AuditRecord) error
	LastAudit(ctx context.Context) (*AuditRecord, error)
	ListAudit(ctx context.Context, fromSeq uint64, limit int) ([]*AuditRecord, error)

	Ping(ctx context.Context) error
	Close() error
}
