package contain

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Claims struct {
	Agent   string `json:"agt"`
	Tenant  string `json:"ten"`
	Session string `json:"ses,omitempty"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
	ID      string `json:"jti"`
	Scope   string `json:"scp,omitempty"`
}

type Issuer struct {
	key []byte
	TTL time.Duration
}

func NewIssuer(key []byte, ttl time.Duration) *Issuer {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &Issuer{key: key, TTL: ttl}
}

var b64 = base64.RawURLEncoding

func (i *Issuer) Issue(agent, tenant, session string, ttl time.Duration) (string, Claims) {
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = i.TTL
	}
	now := time.Now()
	jti := make([]byte, 8)
	rand.Read(jti)
	c := Claims{Agent: agent, Tenant: tenant, Session: session, Issued: now.UnixMilli(), Expires: now.Add(ttl).Unix(), ID: hex.EncodeToString(jti)}
	body, _ := json.Marshal(c)
	payload := b64.EncodeToString(body)
	m := hmac.New(sha256.New, i.key)
	m.Write([]byte("rgt1." + payload))
	return "rgt1." + payload + "." + b64.EncodeToString(m.Sum(nil)), c
}

var ErrBadToken = errors.New("invalid agent token")
var ErrExpired = errors.New("agent token expired")

func (i *Issuer) Verify(tok string) (*Claims, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] != "rgt1" {
		return nil, ErrBadToken
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return nil, ErrBadToken
	}
	m := hmac.New(sha256.New, i.key)
	m.Write([]byte("rgt1." + parts[1]))
	if !hmac.Equal(sig, m.Sum(nil)) {
		return nil, ErrBadToken
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrBadToken
	}
	var c Claims
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, ErrBadToken
	}
	if time.Now().Unix() > c.Expires {
		return nil, ErrExpired
	}
	return &c, nil
}

func HashKey(key string) string {
	h := sha256.Sum256([]byte("rungated-agent-key:" + key))
	return hex.EncodeToString(h[:])
}

func NewAgentKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "rgk_" + b64.EncodeToString(b)
}
