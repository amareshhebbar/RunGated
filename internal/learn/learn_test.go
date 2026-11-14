package learn

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

func toolEv(tool string, args map[string]any) core.Event {
	return core.Event{AgentID: "a1", Action: &core.Action{AgentID: "a1", Kind: core.KindToolCall, Server: "orders", Tool: tool, Args: args}}
}

func TestSuggestInfersTools(t *testing.T) {
	var evs []core.Event
	for i := 0; i < 30; i++ {
		evs = append(evs, toolEv("lookup_order", map[string]any{"order_id": fmt.Sprintf("ORD-%05d", 10000+i), "status": []string{"open", "closed"}[i%2]}))
		evs = append(evs, toolEv("issue_refund", map[string]any{"order_id": fmt.Sprintf("ORD-%05d", 20000+i), "amount": float64(10 + i)}))
	}
	evs = append(evs, core.Event{AgentID: "a1", Action: &core.Action{Kind: core.KindLLMRequest, Provider: "openai", Model: "gpt-4o-2024-08-06"}})
	evs = append(evs, core.Event{AgentID: "a1", Action: &core.Action{Kind: core.KindEgressRequest, Host: "API.Example.com"}})
	evs = append(evs, core.Event{AgentID: "a1",
		Action:   &core.Action{Kind: core.KindToolCall, Server: "orders", Tool: "evil_tool", Args: map[string]any{"x": "y"}},
		Decision: &core.Decision{Findings: []core.Finding{{Category: core.CatInjection, Severity: 0.9}}}})
	evs = append(evs, core.Event{AgentID: "other", Action: &core.Action{Kind: core.KindToolCall, Tool: "not_mine"}})

	m, rep := Suggest(evs, Options{AgentID: "a1", Tenant: "default"})
	if rep.SkippedRisk != 1 {
		t.Fatalf("skipped %d", rep.SkippedRisk)
	}
	if len(m.Spec.Tools) != 2 {
		t.Fatalf("tools %+v", m.Spec.Tools)
	}
	ref := m.FindTool("orders", "issue_refund")
	if ref == nil || ref.BlastRadius != "high" {
		t.Fatalf("refund %+v", ref)
	}
	amt := ref.Args["amount"]
	if amt.Type != "integer" || !amt.Required || amt.Max == nil || *amt.Max < 39 || *amt.Min != 0 {
		t.Fatalf("amount %+v", amt)
	}
	oid := ref.Args["order_id"]
	if oid.Pattern != "^ORD-[0-9]{5}$" {
		t.Fatalf("pattern %q", oid.Pattern)
	}
	look := m.FindTool("orders", "lookup_order")
	if look.BlastRadius != "low" || strings.Join(look.Args["status"].Enum, ",") != "closed,open" {
		t.Fatalf("lookup %+v", look)
	}
	if strings.Join(m.Spec.Model.Allowed, ",") != "gpt-4o*" {
		t.Fatalf("models %v", m.Spec.Model.Allowed)
	}
	if strings.Join(m.Spec.Egress.Allow, ",") != "api.example.com" {
		t.Fatalf("hosts %v", m.Spec.Egress.Allow)
	}
	m.Metadata.ID = "a1"
	y, err := m.YAML()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Parse(y); err != nil {
		t.Fatalf("does not validate: %v\n%s", err, y)
	}
}

func TestSuggestEmptyStillValid(t *testing.T) {
	m, rep := Suggest(nil, Options{AgentID: "a1", Tenant: "default"})
	m.Metadata.ID = "a1"
	y, _ := m.YAML()
	if _, err := manifest.Parse(y); err != nil {
		t.Fatalf("%v\n%s", err, y)
	}
	if len(rep.Notes) == 0 {
		t.Fatal("expected notes")
	}
}

func TestOptionalArgAndHiddenArgs(t *testing.T) {
	evs := []core.Event{
		toolEv("search_docs", map[string]any{"q": "refund policy", "top_k": float64(5)}),
		toolEv("search_docs", map[string]any{"q": "shipping times to Pune"}),
		{AgentID: "a1", Action: &core.Action{Kind: core.KindToolCall, Server: "orders", Tool: "send_email"}},
	}
	m, _ := Suggest(evs, Options{AgentID: "a1"})
	s := m.FindTool("orders", "search_docs")
	if s.Args["top_k"].Required || !s.Args["q"].Required {
		t.Fatalf("%+v", s.Args)
	}
	if s.Args["q"].MaxLength == 0 || s.Args["q"].Pattern != "" {
		t.Fatalf("q %+v", s.Args["q"])
	}
	e := m.FindTool("orders", "send_email")
	if !e.ExtraArgs || e.BlastRadius != "high" {
		t.Fatalf("%+v", e)
	}
}

func TestShapes(t *testing.T) {
	cases := map[string]string{
		"ORD-12345":    "^ORD-[0-9]{5}$",
		"12345":        "^[0-9]{5}$",
		"hello world":  "",
		"cus_ab12":     "^[a-z]{3}_[a-z]{2}[0-9]{2}$",
		"abc":          "",
		"INV2024-0001": "^INV[0-9]{4}-[0-9]{4}$",
	}
	for in, want := range cases {
		if got := shapePattern(shape(in)); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestGuessBlast(t *testing.T) {
	for name, want := range map[string]string{"deleteUser": "high", "get_balance": "low", "summarize": "medium", "send-email": "high"} {
		if got := guessBlast(name); got != want {
			t.Errorf("%s: %s", name, got)
		}
	}
}
