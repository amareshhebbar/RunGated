package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/amareshhebbar/rungated/internal/contain"
)

type Identity struct {
	AgentID   string
	SessionID string
	Tenant    string
	Method    string
}

type KeyChecker interface {
	CheckKey(agentID, key string) bool
}

type Resolver struct {
	Issuer      *contain.Issuer
	Contain     *contain.Service
	Keys        KeyChecker
	RequireAuth bool
}

var ErrNoIdentity = errors.New("no agent identity: send X-Rungated-Token or X-Rungated-Agent with X-Rungated-Key")
var ErrRevoked = errors.New("agent token revoked by containment")

func bearer(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func (r *Resolver) fromToken(tok string) (*Identity, error) {
	c, err := r.Issuer.Verify(tok)
	if err != nil {
		return nil, err
	}
	if r.Contain != nil && r.Contain.TokenRevoked(c) {
		return nil, ErrRevoked
	}
	return &Identity{AgentID: c.Agent, SessionID: c.Session, Tenant: c.Tenant, Method: "token"}, nil
}

func IsAgentToken(s string) bool { return strings.HasPrefix(s, "rgt1.") }

func (r *Resolver) Resolve(req *http.Request) (*Identity, error) {
	sess := req.Header.Get("X-Rungated-Session")
	tok := req.Header.Get("X-Rungated-Token")
	if tok == "" {
		if b := bearer(req.Header.Get("Authorization")); IsAgentToken(b) {
			tok = b
		}
	}
	if tok == "" {
		if k := req.Header.Get("X-Api-Key"); IsAgentToken(k) {
			tok = k
		}
	}
	if tok != "" {
		id, err := r.fromToken(tok)
		if err != nil {
			return nil, err
		}
		if sess != "" && id.SessionID == "" {
			id.SessionID = sess
		}
		return id, nil
	}
	agent := req.Header.Get("X-Rungated-Agent")
	if agent == "" {
		return nil, ErrNoIdentity
	}
	key := req.Header.Get("X-Rungated-Key")
	if key != "" {
		if r.Keys == nil || !r.Keys.CheckKey(agent, key) {
			return nil, errors.New("invalid agent key")
		}
		return &Identity{AgentID: agent, SessionID: sess, Method: "key"}, nil
	}
	if r.RequireAuth {
		return nil, ErrNoIdentity
	}
	return &Identity{AgentID: agent, SessionID: sess, Method: "header"}, nil
}

func (r *Resolver) ResolveProxyAuth(h string, req *http.Request) (*Identity, error) {
	if h != "" && strings.HasPrefix(strings.ToLower(h), "basic ") {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(h[6:]))
		if err != nil {
			return nil, errors.New("bad proxy authorization")
		}
		user, pass, _ := strings.Cut(string(raw), ":")
		if IsAgentToken(pass) {
			id, err := r.fromToken(pass)
			if err != nil {
				return nil, err
			}
			if user != "" && user != id.AgentID && !strings.HasPrefix(user, id.AgentID+"@") {
				return nil, errors.New("proxy user does not match token")
			}
			if _, s, ok := strings.Cut(user, "@"); ok && id.SessionID == "" {
				id.SessionID = s
			}
			return id, nil
		}
		agent, sess, _ := strings.Cut(user, "@")
		if strings.HasPrefix(pass, "rgk_") {
			if r.Keys == nil || !r.Keys.CheckKey(agent, pass) {
				return nil, errors.New("invalid agent key")
			}
			return &Identity{AgentID: agent, SessionID: sess, Method: "key"}, nil
		}
		if !r.RequireAuth && agent != "" {
			return &Identity{AgentID: agent, SessionID: sess, Method: "header"}, nil
		}
		return nil, ErrNoIdentity
	}
	if req != nil {
		return r.Resolve(req)
	}
	return nil, ErrNoIdentity
}

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleSecurity  Role = "security"
	RoleDeveloper Role = "developer"
	RoleAuditor   Role = "auditor"
)

type Principal struct {
	Name   string
	Role   Role
	Tenant string
}

type APIKeys struct {
	Keys     map[string]Principal
	Disabled bool
}

func (k *APIKeys) Authenticate(req *http.Request) (*Principal, bool) {
	if k == nil || k.Disabled {
		return &Principal{Name: "anonymous", Role: RoleAdmin}, true
	}
	tok := bearer(req.Header.Get("Authorization"))
	if tok == "" {
		tok = req.URL.Query().Get("access_token")
	}
	if tok == "" {
		if c, err := req.Cookie("rungated_session"); err == nil {
			tok = c.Value
		}
	}
	for key, p := range k.Keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(tok)) == 1 {
			pp := p
			return &pp, true
		}
	}
	return nil, false
}

func (p *Principal) Can(action string) bool {
	switch p.Role {
	case RoleAdmin:
		return true
	case RoleSecurity:
		return action != "admin"
	case RoleDeveloper:
		return action == "read" || action == "register" || action == "decide"
	case RoleAuditor:
		return action == "read" || action == "audit"
	}
	return false
}
