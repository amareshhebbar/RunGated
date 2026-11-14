package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/amareshhebbar/rungated/internal/core"
)

const schemaSQL = `
create table if not exists rg_agents (id text primary key, tenant text not null, updated_at timestamptz not null, doc jsonb not null);
create index if not exists ab_agents_tenant on rg_agents (tenant);
create table if not exists rg_policies (tenant text not null, version int not null, source text not null, author text not null, updated_at timestamptz not null, primary key (tenant, version));
create table if not exists rg_approvals (id text primary key, tenant text not null, agent_id text not null, status text not null, created_at timestamptz not null, doc jsonb not null);
create index if not exists ab_approvals_status on rg_approvals (status, created_at desc);
create table if not exists rg_pins (key text primary key, tenant text not null, doc jsonb not null);
create table if not exists rg_incidents (id text primary key, tenant text not null, agent_id text not null, status text not null, updated_at timestamptz not null, doc jsonb not null);
create index if not exists ab_incidents_updated on rg_incidents (updated_at desc);
create table if not exists rg_containments (id text primary key, active boolean not null, created_at timestamptz not null, doc jsonb not null);
create table if not exists rg_events (id text primary key, tenant text not null, agent_id text not null, session_id text not null, type text not null, verdict_rank int not null, computed_rank int not null, time timestamptz not null, doc jsonb not null);
create index if not exists ab_events_time on rg_events (time desc);
create index if not exists ab_events_agent on rg_events (agent_id, time desc);
create index if not exists ab_events_session on rg_events (session_id, time desc);
create table if not exists ab_audit_log (seq bigint primary key, time timestamptz not null, event_id text not null, tenant text not null, agent_id text not null, type text not null, verdict text not null, score int not null, event_digest text not null, prev_hash text not null, hash text not null);
create or replace function ab_audit_immutable() returns trigger as $$ begin raise exception 'ab_audit_log is append only'; end; $$ language plpgsql;
drop trigger if exists ab_audit_immutable on ab_audit_log;
create trigger ab_audit_immutable before update or delete on ab_audit_log for each row execute function ab_audit_immutable();
`

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(ctx context.Context, dsn string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConns < 8 {
		cfg.MaxConns = 16
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i := 0; i < 30; i++ {
		if lastErr = pool.Ping(ctx); lastErr == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if lastErr != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres not reachable: %w", lastErr)
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

func (p *Postgres) upsert(ctx context.Context, sql string, args ...any) error {
	_, err := p.pool.Exec(ctx, sql, args...)
	return err
}

func getDoc[T any](ctx context.Context, p *Postgres, sql string, args ...any) (*T, error) {
	var b []byte
	if err := p.pool.QueryRow(ctx, sql, args...).Scan(&b); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var out T
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func listDocs[T any](ctx context.Context, p *Postgres, sql string, args ...any) ([]*T, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*T
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

func (p *Postgres) PutAgent(ctx context.Context, a *AgentRecord) error {
	b, _ := json.Marshal(a)
	return p.upsert(ctx, `insert into rg_agents (id, tenant, updated_at, doc) values ($1,$2,$3,$4) on conflict (id) do update set tenant=excluded.tenant, updated_at=excluded.updated_at, doc=excluded.doc`, a.ID, a.Tenant, a.UpdatedAt, b)
}

func (p *Postgres) GetAgent(ctx context.Context, id string) (*AgentRecord, error) {
	return getDoc[AgentRecord](ctx, p, `select doc from rg_agents where id=$1`, id)
}

func (p *Postgres) ListAgents(ctx context.Context, tenant string) ([]*AgentRecord, error) {
	return listDocs[AgentRecord](ctx, p, `select doc from rg_agents where ($1 = '' or tenant = $1) order by id`, tenant)
}

func (p *Postgres) DeleteAgent(ctx context.Context, id string) error {
	t, err := p.pool.Exec(ctx, `delete from rg_agents where id=$1`, id)
	if err != nil {
		return err
	}
	if t.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) PutPolicy(ctx context.Context, tenant, src, author string) (*PolicyRecord, error) {
	r := &PolicyRecord{Tenant: tenant, Source: src, Author: author, UpdatedAt: time.Now().UTC()}
	err := p.pool.QueryRow(ctx, `insert into rg_policies (tenant, version, source, author, updated_at) values ($1, coalesce((select max(version) from rg_policies where tenant=$1),0)+1, $2, $3, $4) returning version`, tenant, src, author, r.UpdatedAt).Scan(&r.Version)
	return r, err
}

func (p *Postgres) scanPolicies(ctx context.Context, sql string, args ...any) ([]*PolicyRecord, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PolicyRecord
	for rows.Next() {
		r := &PolicyRecord{}
		if err := rows.Scan(&r.Tenant, &r.Version, &r.Source, &r.Author, &r.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) GetPolicy(ctx context.Context, tenant string) (*PolicyRecord, error) {
	out, err := p.scanPolicies(ctx, `select tenant, version, source, author, updated_at from rg_policies where tenant=$1 order by version desc limit 1`, tenant)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out[0], nil
}

func (p *Postgres) ListPolicies(ctx context.Context) ([]*PolicyRecord, error) {
	return p.scanPolicies(ctx, `select distinct on (tenant) tenant, version, source, author, updated_at from rg_policies order by tenant, version desc`)
}

func (p *Postgres) PolicyHistory(ctx context.Context, tenant string) ([]*PolicyRecord, error) {
	return p.scanPolicies(ctx, `select tenant, version, source, author, updated_at from rg_policies where tenant=$1 order by version`, tenant)
}

func (p *Postgres) PutApproval(ctx context.Context, a *Approval) error {
	b, _ := json.Marshal(a)
	return p.upsert(ctx, `insert into rg_approvals (id, tenant, agent_id, status, created_at, doc) values ($1,$2,$3,$4,$5,$6) on conflict (id) do update set status=excluded.status, doc=excluded.doc`, a.ID, a.Tenant, a.AgentID, a.Status, a.CreatedAt, b)
}

func (p *Postgres) GetApproval(ctx context.Context, id string) (*Approval, error) {
	return getDoc[Approval](ctx, p, `select doc from rg_approvals where id=$1`, id)
}

func lim(n int) int {
	if n <= 0 || n > 5000 {
		return 500
	}
	return n
}

func (p *Postgres) ListApprovals(ctx context.Context, f ApprovalFilter) ([]*Approval, error) {
	return listDocs[Approval](ctx, p, `select doc from rg_approvals where ($1='' or tenant=$1) and ($2='' or status=$2) and ($3='' or agent_id=$3) order by created_at desc limit $4`, f.Tenant, f.Status, f.AgentID, lim(f.Limit))
}

func (p *Postgres) PutPin(ctx context.Context, pin *Pin) error {
	b, _ := json.Marshal(pin)
	return p.upsert(ctx, `insert into rg_pins (key, tenant, doc) values ($1,$2,$3) on conflict (key) do update set doc=excluded.doc`, pin.Key(), pin.Tenant, b)
}

func (p *Postgres) GetPin(ctx context.Context, tenant, server, tool string) (*Pin, error) {
	return getDoc[Pin](ctx, p, `select doc from rg_pins where key=$1`, tenant+"/"+server+"/"+tool)
}

func (p *Postgres) ListPins(ctx context.Context, tenant string) ([]*Pin, error) {
	return listDocs[Pin](ctx, p, `select doc from rg_pins where ($1='' or tenant=$1) order by key`, tenant)
}

func (p *Postgres) PutIncident(ctx context.Context, i *Incident) error {
	b, _ := json.Marshal(i)
	return p.upsert(ctx, `insert into rg_incidents (id, tenant, agent_id, status, updated_at, doc) values ($1,$2,$3,$4,$5,$6) on conflict (id) do update set status=excluded.status, updated_at=excluded.updated_at, doc=excluded.doc`, i.ID, i.Tenant, i.AgentID, i.Status, i.UpdatedAt, b)
}

func (p *Postgres) GetIncident(ctx context.Context, id string) (*Incident, error) {
	return getDoc[Incident](ctx, p, `select doc from rg_incidents where id=$1`, id)
}

func (p *Postgres) ListIncidents(ctx context.Context, f IncidentFilter) ([]*Incident, error) {
	return listDocs[Incident](ctx, p, `select doc from rg_incidents where ($1='' or tenant=$1) and ($2='' or status=$2) and ($3='' or agent_id=$3) order by updated_at desc limit $4`, f.Tenant, f.Status, f.AgentID, lim(f.Limit))
}

func (p *Postgres) PutContainment(ctx context.Context, c *Containment) error {
	b, _ := json.Marshal(c)
	return p.upsert(ctx, `insert into rg_containments (id, active, created_at, doc) values ($1,$2,$3,$4) on conflict (id) do update set active=excluded.active, doc=excluded.doc`, c.ID, c.ReleasedAt == nil, c.CreatedAt, b)
}

func (p *Postgres) ListContainments(ctx context.Context, activeOnly bool) ([]*Containment, error) {
	return listDocs[Containment](ctx, p, `select doc from rg_containments where (not $1 or active) order by created_at desc`, activeOnly)
}

func (p *Postgres) AppendEvents(ctx context.Context, evs []core.Event) error {
	if len(evs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, e := range evs {
		b, _ := json.Marshal(e)
		vr, cr := 0, 0
		if e.Decision != nil {
			vr, cr = e.Decision.Verdict.Rank(), e.Decision.Computed.Rank()
		}
		batch.Queue(`insert into rg_events (id, tenant, agent_id, session_id, type, verdict_rank, computed_rank, time, doc) values ($1,$2,$3,$4,$5,$6,$7,$8,$9) on conflict (id) do update set doc=excluded.doc, verdict_rank=excluded.verdict_rank, computed_rank=excluded.computed_rank`, e.ID, e.TenantID, e.AgentID, e.SessionID, e.Type, vr, cr, e.Time, b)
	}
	return p.pool.SendBatch(ctx, batch).Close()
}

func (p *Postgres) QueryEvents(ctx context.Context, f EventFilter) ([]core.Event, error) {
	if len(f.IDs) > 0 {
		out, err := listDocs[core.Event](ctx, p, `select doc from rg_events where id = any($1) order by time`, f.IDs)
		return deref(out), err
	}
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.Tenant != "" {
		add("tenant = $%d", f.Tenant)
	}
	if f.AgentID != "" {
		add("agent_id = $%d", f.AgentID)
	}
	if f.SessionID != "" {
		add("session_id = $%d", f.SessionID)
	}
	if f.Type != "" {
		add("type = $%d", f.Type)
	}
	if !f.Since.IsZero() {
		add("time >= $%d", f.Since)
	}
	if !f.Before.IsZero() {
		add("time < $%d", f.Before)
	}
	if f.MinVerdict != "" {
		r := core.Verdict(f.MinVerdict).Rank()
		args = append(args, r)
		where = append(where, fmt.Sprintf("(verdict_rank >= $%d or computed_rank >= $%d)", len(args), len(args)))
	}
	q := "select doc from rg_events"
	if len(where) > 0 {
		q += " where " + strings.Join(where, " and ")
	}
	args = append(args, lim(f.Limit))
	q += fmt.Sprintf(" order by time desc limit $%d", len(args))
	out, err := listDocs[core.Event](ctx, p, q, args...)
	return deref(out), err
}

func deref(in []*core.Event) []core.Event {
	out := make([]core.Event, len(in))
	for i, e := range in {
		out[i] = *e
	}
	return out
}

func (p *Postgres) AppendAudit(ctx context.Context, r *AuditRecord) error {
	_, err := p.pool.Exec(ctx, `insert into ab_audit_log (seq, time, event_id, tenant, agent_id, type, verdict, score, event_digest, prev_hash, hash) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, int64(r.Seq), r.Time, r.EventID, r.Tenant, r.AgentID, r.Type, r.Verdict, r.Score, r.EventDigest, r.PrevHash, r.Hash)
	return err
}

func (p *Postgres) scanAudit(ctx context.Context, sql string, args ...any) ([]*AuditRecord, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AuditRecord
	for rows.Next() {
		r := &AuditRecord{}
		var seq int64
		if err := rows.Scan(&seq, &r.Time, &r.EventID, &r.Tenant, &r.AgentID, &r.Type, &r.Verdict, &r.Score, &r.EventDigest, &r.PrevHash, &r.Hash); err != nil {
			return nil, err
		}
		r.Seq = uint64(seq)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *Postgres) LastAudit(ctx context.Context) (*AuditRecord, error) {
	out, err := p.scanAudit(ctx, `select seq, time, event_id, tenant, agent_id, type, verdict, score, event_digest, prev_hash, hash from ab_audit_log order by seq desc limit 1`)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out[0], nil
}

func (p *Postgres) ListAudit(ctx context.Context, fromSeq uint64, limit int) ([]*AuditRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	return p.scanAudit(ctx, `select seq, time, event_id, tenant, agent_id, type, verdict, score, event_digest, prev_hash, hash from ab_audit_log where seq >= $1 order by seq limit $2`, int64(fromSeq), limit)
}

func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
