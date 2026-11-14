package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	free := 0
	for _, m := range Catalog {
		if seen[m.ID] {
			t.Fatalf("duplicate %s", m.ID)
		}
		seen[m.ID] = true
		if m.Tier == TierFree {
			free++
			if !m.Local || m.Repo == "" {
				t.Fatalf("%s: free models must be downloadable", m.ID)
			}
		} else if m.Local {
			t.Fatalf("%s: trial models are hosted only", m.ID)
		}
	}
	if free < 2 {
		t.Fatal("need at least two free models")
	}
	if !strings.Contains(Table(), "rungated-embed-mini") {
		t.Fatal("table")
	}
}

func TestTrialLeft(t *testing.T) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if d, ok := TrialLeft(TierTrial14, start, start.Add(time.Hour)); !ok || d != 14 {
		t.Fatalf("day one: %d %v", d, ok)
	}
	if d, ok := TrialLeft(TierTrial14, start, start.Add(13*24*time.Hour+time.Hour)); !ok || d != 1 {
		t.Fatalf("last day: %d %v", d, ok)
	}
	if _, ok := TrialLeft(TierTrial14, start, start.Add(14*24*time.Hour)); ok {
		t.Fatal("expired trial still active")
	}
	if _, ok := TrialLeft(TierTrial30, start, start.Add(29*24*time.Hour)); !ok {
		t.Fatal("30 day trial ended early")
	}
	if d, ok := TrialLeft(TierFree, start, start.Add(9999*time.Hour)); !ok || d != -1 {
		t.Fatal("free tier must never expire")
	}
}

func TestPull(t *testing.T) {
	model := []byte("onnx bytes")
	tok := []byte(`{"model":{}}`)
	sum := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	man, _ := json.Marshal(Manifest{ID: "rungated-embed-mini", Files: []File{{Name: "model.int8.onnx", SHA256: sum(model)}, {Name: "tokenizer.json", SHA256: sum(tok)}}})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/"+ManifestName):
			w.Write(man)
		case strings.HasSuffix(r.URL.Path, "/model.int8.onnx"):
			w.Write(model)
		case strings.HasSuffix(r.URL.Path, "/tokenizer.json"):
			w.Write(tok)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := HubURL
	HubURL = srv.URL
	defer func() { HubURL = old }()
	dir := t.TempDir()
	p := &Puller{}
	got, err := p.Pull(context.Background(), "rungated-embed-mini", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(got, "model.int8.onnx"))
	if string(b) != string(model) {
		t.Fatal("model content")
	}
	f, err := ModelFile(dir, "rungated-embed-mini")
	if err != nil || filepath.Base(f) != "model.int8.onnx" {
		t.Fatalf("model file %s %v", f, err)
	}
	if _, err := p.Pull(context.Background(), "rungated-guard-large", dir); err == nil || !strings.Contains(err.Error(), "hosted only") {
		t.Fatalf("hosted model pull: %v", err)
	}
	model = []byte("tampered")
	os.Remove(filepath.Join(got, "model.int8.onnx"))
	if _, err := p.Pull(context.Background(), "rungated-embed-mini", dir); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
}
