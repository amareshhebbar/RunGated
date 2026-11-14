package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

const QuarantineText = "[[QUARANTINED: untrusted content removed by Rungated because it contained instructions aimed at the AI agent]]"

var TokenRe = regexp.MustCompile(`\[\[(?:PII|PCI|PHI|SECRET|DATA|FINANCIAL)_[A-Z0-9_]{1,40}_[0-9A-F]{8}\]\]`)

type entry struct {
	nonce  []byte
	sealed []byte
	class  string
}

type session struct {
	tokens   map[string]entry
	byMAC    map[string]string
	lastUsed time.Time
}

type Vault struct {
	mu       sync.Mutex
	master   []byte
	keys     map[string][]byte
	sessions map[string]*session
	TTL      time.Duration
}

func New(master []byte) (*Vault, error) {
	if len(master) < 32 {
		return nil, errors.New("vault master key must be at least 32 bytes")
	}
	return &Vault{master: append([]byte{}, master...), keys: map[string][]byte{}, sessions: map[string]*session{}, TTL: 24 * time.Hour}, nil
}

func NewRandomKey() []byte {
	b := make([]byte, 32)
	rand.Read(b)
	return b
}

func (v *Vault) tenantKey(tenant string) []byte {
	if k, ok := v.keys[tenant]; ok {
		return k
	}
	k, err := hkdf.Key(sha256.New, v.master, []byte("rungated-vault-v1"), "tenant:"+tenant, 32)
	if err != nil {
		panic(err)
	}
	v.keys[tenant] = k
	return k
}

func sessKey(tenant, sess string) string { return tenant + "\x00" + sess }

func (v *Vault) sess(tenant, sid string) *session {
	k := sessKey(tenant, sid)
	s := v.sessions[k]
	if s == nil {
		s = &session{tokens: map[string]entry{}, byMAC: map[string]string{}}
		v.sessions[k] = s
	}
	s.lastUsed = time.Now()
	return s
}

func classLabel(class string) string {
	c := strings.ToUpper(class)
	c = strings.NewReplacer(".", "_", "-", "_", "*", "").Replace(c)
	if !strings.HasPrefix(c, "PII_") && !strings.HasPrefix(c, "PCI_") && !strings.HasPrefix(c, "PHI_") && !strings.HasPrefix(c, "SECRET_") && !strings.HasPrefix(c, "DATA_") && !strings.HasPrefix(c, "FINANCIAL_") {
		c = "DATA_" + c
	}
	if len(c) > 40 {
		c = c[:40]
	}
	return c
}

func (v *Vault) Tokenize(tenant, sid, class, value string) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	key := v.tenantKey(tenant)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(sid))
	mac.Write([]byte{0})
	mac.Write([]byte(value))
	sum := mac.Sum(nil)
	macHex := hex.EncodeToString(sum)
	s := v.sess(tenant, sid)
	if tok, ok := s.byMAC[macHex]; ok {
		return tok, nil
	}
	tok := "[[" + classLabel(class) + "_" + strings.ToUpper(hex.EncodeToString(sum[:4])) + "]]"
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	sealed := gcm.Seal(nil, nonce, []byte(value), []byte(sid+"|"+tok))
	s.tokens[tok] = entry{nonce: nonce, sealed: sealed, class: class}
	s.byMAC[macHex] = tok
	return tok, nil
}

func (v *Vault) lookup(tenant, sid, tok string) (string, bool) {
	s := v.sessions[sessKey(tenant, sid)]
	if s == nil {
		return "", false
	}
	e, ok := s.tokens[tok]
	if !ok {
		return "", false
	}
	block, err := aes.NewCipher(v.tenantKey(tenant))
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	pt, err := gcm.Open(nil, e.nonce, e.sealed, []byte(sid+"|"+tok))
	if err != nil {
		return "", false
	}
	s.lastUsed = time.Now()
	return string(pt), true
}

func (v *Vault) Restore(tenant, sid, text string) string {
	if !strings.Contains(text, "[[") {
		return text
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return TokenRe.ReplaceAllStringFunc(text, func(tok string) string {
		if val, ok := v.lookup(tenant, sid, tok); ok {
			return val
		}
		return tok
	})
}

func (v *Vault) RestoreAny(tenant, sid string, x any) any {
	switch t := x.(type) {
	case string:
		return v.Restore(tenant, sid, t)
	case map[string]any:
		for k, val := range t {
			t[k] = v.RestoreAny(tenant, sid, val)
		}
		return t
	case []any:
		for i := range t {
			t[i] = v.RestoreAny(tenant, sid, t[i])
		}
		return t
	}
	return x
}

type Span struct {
	Start int
	End   int
	Class string
}

func (v *Vault) Redact(tenant, sid string, part int, text string, spans []Span) (string, []core.Redaction, error) {
	if len(spans) == 0 {
		return text, nil, nil
	}
	for _, s := range spans {
		if s.Class == "untrusted.injection" {
			return QuarantineText, []core.Redaction{{Part: part, Start: 0, End: len(text), Token: "QUARANTINED", Class: s.Class}}, nil
		}
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	var merged []Span
	for _, s := range spans {
		if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
			continue
		}
		if n := len(merged); n > 0 && s.Start < merged[n-1].End {
			if s.End > merged[n-1].End {
				merged[n-1].End = s.End
			}
			continue
		}
		merged = append(merged, s)
	}
	var b strings.Builder
	var reds []core.Redaction
	last := 0
	for _, s := range merged {
		tok, err := v.Tokenize(tenant, sid, s.Class, text[s.Start:s.End])
		if err != nil {
			return text, nil, err
		}
		b.WriteString(text[last:s.Start])
		b.WriteString(tok)
		reds = append(reds, core.Redaction{Part: part, Start: s.Start, End: s.End, Token: tok, Class: s.Class})
		last = s.End
	}
	b.WriteString(text[last:])
	return b.String(), reds, nil
}

func (v *Vault) Purge(tenant, sid string) {
	v.mu.Lock()
	delete(v.sessions, sessKey(tenant, sid))
	v.mu.Unlock()
}

func (v *Vault) GC() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	n := 0
	cut := time.Now().Add(-v.TTL)
	for k, s := range v.sessions {
		if s.lastUsed.Before(cut) {
			delete(v.sessions, k)
			n++
		}
	}
	return n
}

func (v *Vault) Stats() map[string]int {
	v.mu.Lock()
	defer v.mu.Unlock()
	tokens := 0
	for _, s := range v.sessions {
		tokens += len(s.tokens)
	}
	return map[string]int{"sessions": len(v.sessions), "tokens": tokens}
}

type StreamRestorer struct {
	v       *Vault
	tenant  string
	sid     string
	pending string
}

func (v *Vault) NewStreamRestorer(tenant, sid string) *StreamRestorer {
	return &StreamRestorer{v: v, tenant: tenant, sid: sid}
}

const maxTokenLen = 64

func (r *StreamRestorer) Push(chunk string) string {
	buf := r.pending + chunk
	r.pending = ""
	idx := strings.LastIndex(buf, "[")
	if idx >= 0 && !strings.Contains(buf[idx:], "]]") && len(buf)-idx < maxTokenLen {
		if idx > 0 && buf[idx-1] == '[' {
			idx--
		}
		tail := buf[idx:]
		if tail == "[" || strings.HasPrefix(tail, "[[") {
			r.pending = tail
			buf = buf[:idx]
		}
	}
	if strings.Count(buf, "[[") > strings.Count(buf, "]]") {
		k := strings.LastIndex(buf, "[[")
		if len(buf)-k < maxTokenLen {
			r.pending = buf[k:] + r.pending
			buf = buf[:k]
		}
	}
	return r.v.Restore(r.tenant, r.sid, buf)
}

func (r *StreamRestorer) Flush() string {
	p := r.pending
	r.pending = ""
	return r.v.Restore(r.tenant, r.sid, p)
}
