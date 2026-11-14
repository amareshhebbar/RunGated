package gateway

import (
	"testing"
	"time"
)

func TestIncidentsGCAndCap(t *testing.T) {
	in := NewIncidents(nil)
	now := time.Now().UTC()
	in.open["t/a/old"] = "inc1"
	in.seen["t/a/old"] = now.Add(-2 * time.Hour)
	in.open["t/a/new"] = "inc2"
	in.seen["t/a/new"] = now
	in.open["t/a/orphan"] = "inc3"
	if n := in.GC(now); n != 2 {
		t.Fatalf("want 2 removed, got %d", n)
	}
	if _, ok := in.open["t/a/new"]; !ok {
		t.Fatalf("recent session removed")
	}
	ids := make([]string, maxIncidentEvents+50)
	for i := range ids {
		ids[i] = string(rune('a' + i%26))
	}
	if got := capIDs(ids); len(got) != maxIncidentEvents {
		t.Fatalf("event ids not capped: %d", len(got))
	}
}
