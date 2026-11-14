package gateway

import (
	"testing"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

func TestEgressClassFindings(t *testing.T) {
	m := &manifest.Manifest{}
	m.Spec.DataClasses.Allowed = []string{"public", "pii.email"}
	a := &core.Action{Kind: core.KindEgressRequest, Host: "api.acme.example"}
	fs := []core.Finding{{DataClass: "pii.email"}, {DataClass: "pii.passport"}, {DataClass: "pii.passport"}, {DataClass: "untrusted.injection"}, {DataClass: "pii.iban"}}
	out := egressClassFindings(a, m, fs)
	if len(out) != 2 {
		t.Fatalf("want passport and iban, got %+v", out)
	}
	for _, f := range out {
		if f.Min != core.VerdictHold || f.Rule != "data_class.not_allowed" {
			t.Fatalf("bad finding %+v", f)
		}
	}
	if got := egressClassFindings(&core.Action{Kind: core.KindToolCall}, m, fs); got != nil {
		t.Fatalf("only egress requests are guarded, got %+v", got)
	}
	open := &manifest.Manifest{}
	if got := egressClassFindings(a, open, fs); got != nil {
		t.Fatalf("no allowed list means no change in behaviour, got %+v", got)
	}
}
