package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

func EventDigest(e core.Event) string {
	b, _ := json.Marshal(e)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func ChainHash(r *store.AuditRecord) string {
	h := sha256.New()
	for _, s := range []string{r.PrevHash, strconv.FormatUint(r.Seq, 10), r.Time.UTC().Format(time.RFC3339Nano), r.EventID, r.Tenant, r.AgentID, r.Type, r.Verdict, strconv.Itoa(r.Score), r.EventDigest} {
		h.Write([]byte(s))
		h.Write([]byte{0x1f})
	}
	return hex.EncodeToString(h.Sum(nil))
}

type Auditor struct {
	mu   sync.Mutex
	st   store.Store
	last *store.AuditRecord
}

func NewAuditor(ctx context.Context, st store.Store) (*Auditor, error) {
	a := &Auditor{st: st}
	last, err := st.LastAudit(ctx)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	a.last = last
	return a, nil
}

func (a *Auditor) Append(ctx context.Context, e core.Event) (*store.AuditRecord, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for attempt := 0; attempt < 5; attempt++ {
		r := &store.AuditRecord{
			Time:        e.Time.UTC(),
			EventID:     e.ID,
			Tenant:      e.TenantID,
			AgentID:     e.AgentID,
			Type:        e.Type,
			EventDigest: EventDigest(e),
			PrevHash:    GenesisHash,
			Seq:         1,
		}
		if e.Decision != nil {
			r.Verdict = string(e.Decision.Verdict)
			r.Score = e.Decision.Score
		}
		if a.last != nil {
			r.PrevHash = a.last.Hash
			r.Seq = a.last.Seq + 1
		}
		r.Hash = ChainHash(r)
		if err := a.st.AppendAudit(ctx, r); err != nil {
			last, lerr := a.st.LastAudit(ctx)
			if lerr != nil {
				return nil, err
			}
			a.last = last
			continue
		}
		a.last = r
		return r, nil
	}
	return nil, fmt.Errorf("audit append failed after retries")
}

type VerifyResult struct {
	OK            bool   `json:"ok"`
	Records       uint64 `json:"records"`
	FirstBadSeq   uint64 `json:"first_bad_seq,omitempty"`
	Error         string `json:"error,omitempty"`
	HeadHash      string `json:"head_hash,omitempty"`
	EventsChecked int    `json:"events_checked"`
}

func Verify(ctx context.Context, st store.Store, checkEvents bool) VerifyResult {
	res := VerifyResult{OK: true}
	prev := GenesisHash
	var seq uint64 = 1
	for {
		recs, err := st.ListAudit(ctx, seq, 1000)
		if err != nil {
			return VerifyResult{OK: false, Error: err.Error()}
		}
		if len(recs) == 0 {
			break
		}
		var ids []string
		for _, r := range recs {
			if r.Seq != seq {
				return VerifyResult{OK: false, Records: res.Records, FirstBadSeq: seq, Error: fmt.Sprintf("gap in sequence: expected %d got %d", seq, r.Seq)}
			}
			if r.PrevHash != prev {
				return VerifyResult{OK: false, Records: res.Records, FirstBadSeq: r.Seq, Error: "previous hash mismatch"}
			}
			if ChainHash(r) != r.Hash {
				return VerifyResult{OK: false, Records: res.Records, FirstBadSeq: r.Seq, Error: "record hash mismatch (record was modified)"}
			}
			prev = r.Hash
			seq++
			res.Records++
			ids = append(ids, r.EventID)
		}
		if checkEvents {
			evs, err := st.QueryEvents(ctx, store.EventFilter{IDs: ids})
			if err == nil {
				byID := map[string]core.Event{}
				for _, e := range evs {
					byID[e.ID] = e
				}
				for _, r := range recs {
					e, ok := byID[r.EventID]
					if !ok {
						continue
					}
					res.EventsChecked++
					if EventDigest(e) != r.EventDigest {
						return VerifyResult{OK: false, Records: res.Records, FirstBadSeq: r.Seq, Error: "stored event content does not match audit digest", EventsChecked: res.EventsChecked}
					}
				}
			}
		}
	}
	res.HeadHash = prev
	return res
}
