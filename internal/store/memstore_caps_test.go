package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

func TestAuditWindowFallsBackToDisk(t *testing.T) {
	dir := t.TempDir()
	s, err := NewMemStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.maxAudit = 100
	ctx := context.Background()
	for i := 1; i <= 2500; i++ {
		if err := s.AppendAudit(ctx, &AuditRecord{Seq: uint64(i), EventID: fmt.Sprintf("e%d", i), Hash: fmt.Sprintf("h%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.audit) > 125 {
		t.Fatalf("audit window not capped: %d", len(s.audit))
	}
	var seen uint64
	for from := uint64(0); ; {
		page, err := s.ListAudit(ctx, from, 300)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			if r.Seq != seen+1 {
				t.Fatalf("gap or reorder: got %d after %d", r.Seq, seen)
			}
			seen = r.Seq
		}
		from = seen + 1
	}
	if seen != 2500 {
		t.Fatalf("read back %d records, want 2500", seen)
	}
	last, err := s.LastAudit(ctx)
	if err != nil || last.Seq != 2500 {
		t.Fatalf("last audit %v %v", last, err)
	}
	s.Close()
	re, err := NewMemStore(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	re.maxAudit = 100
	page, err := re.ListAudit(ctx, 1, 5)
	if err != nil || len(page) != 5 || page[0].Seq != 1 {
		t.Fatalf("after reload: %v %v", page, err)
	}
}

func TestIncidentAndApprovalCaps(t *testing.T) {
	s, err := NewMemStore("", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.maxIncid, s.maxAppr = 100, 100
	ctx := context.Background()
	base := time.Now().UTC()
	s.PutIncident(ctx, &Incident{ID: "keep-open", Status: "open", UpdatedAt: base})
	for i := 0; i < 300; i++ {
		s.PutIncident(ctx, &Incident{ID: fmt.Sprintf("inc%d", i), Status: "resolved", UpdatedAt: base.Add(time.Duration(i) * time.Second)})
	}
	if n := len(s.st.Incidents); n > 125 {
		t.Fatalf("incidents not capped: %d", n)
	}
	if _, err := s.GetIncident(ctx, "keep-open"); err != nil {
		t.Fatalf("open incident was pruned before resolved ones")
	}
	if _, err := s.GetIncident(ctx, "inc299"); err != nil {
		t.Fatalf("newest incident was pruned")
	}
	s.PutApproval(ctx, &Approval{ID: "pending", Status: "pending", CreatedAt: base})
	for i := 0; i < 300; i++ {
		s.PutApproval(ctx, &Approval{ID: fmt.Sprintf("a%d", i), Status: "approved", CreatedAt: base.Add(time.Duration(i) * time.Second)})
	}
	if n := len(s.st.Approvals); n > 125 {
		t.Fatalf("approvals not capped: %d", n)
	}
	if _, err := s.GetApproval(ctx, "pending"); err != nil {
		t.Fatalf("pending approval was pruned")
	}
}

func TestEventFileRotates(t *testing.T) {
	dir := t.TempDir()
	s, err := NewMemStore(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	s.evRotate = 4096
	ctx := context.Background()
	for i := 0; i < 400; i++ {
		s.AppendEvents(ctx, []core.Event{{ID: fmt.Sprintf("evt%d", i), Type: "decision", Message: "some payload text for size"}})
	}
	if len(s.events) > 62 {
		t.Fatalf("memory window not capped: %d", len(s.events))
	}
	s.Close()
	if _, err := os.Stat(filepath.Join(dir, "events.jsonl.1")); err != nil {
		t.Fatalf("no rotated file: %v", err)
	}
	re, err := NewMemStore(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	evs, _ := re.QueryEvents(ctx, EventFilter{Limit: 1})
	if len(evs) != 1 || evs[0].ID != "evt399" {
		t.Fatalf("newest event not restored after rotation: %+v", evs)
	}
}
