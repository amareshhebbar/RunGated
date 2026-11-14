package server

import (
	"testing"
)

func TestFeedbackLoop(t *testing.T) {
	e := newEnv(t, nil)
	code, _, b := e.do("POST", "/v1/decide", map[string]any{"kind": "tool.call", "tool": "rm_rf", "args": map[string]any{}}, nil)
	if code != 200 {
		t.Fatalf("decide %d %s", code, b)
	}
	actionID, _ := jsonOf(t, b)["action_id"].(string)
	if actionID == "" {
		t.Fatalf("no action id in %s", b)
	}
	var eventID string
	waitFor(t, "decision event", func() bool {
		c, _, body := e.do("GET", "/v1/events?limit=50", nil, nil)
		if c != 200 {
			return false
		}
		evs, _ := jsonOf(t, body)["events"].([]any)
		for _, x := range evs {
			ev := x.(map[string]any)
			if d, ok := ev["decision"].(map[string]any); ok && d["action_id"] == actionID {
				eventID = ev["id"].(string)
				return true
			}
		}
		return false
	})
	code, _, b = e.do("POST", "/v1/feedback", map[string]any{"action_id": actionID, "label": "correct", "note": "real attack"}, nil)
	if code != 201 {
		t.Fatalf("feedback by action %d %s", code, b)
	}
	code, _, b = e.do("POST", "/v1/events/"+eventID+"/feedback", map[string]any{"label": "false_positive", "expected": "allow"}, nil)
	if code != 201 {
		t.Fatalf("feedback by event %d %s", code, b)
	}
	code, _, _ = e.do("POST", "/v1/feedback", map[string]any{"action_id": actionID, "label": "maybe"}, nil)
	if code != 400 {
		t.Fatalf("bad label must be 400, got %d", code)
	}
	code, _, _ = e.do("POST", "/v1/feedback", map[string]any{"action_id": actionID, "label": "correct", "expected": "nuke"}, nil)
	if code != 400 {
		t.Fatalf("bad expected verdict must be 400, got %d", code)
	}
	code, _, _ = e.do("POST", "/v1/events/evt_missing/feedback", map[string]any{"label": "correct"}, nil)
	if code != 404 {
		t.Fatalf("missing event must be 404, got %d", code)
	}
	waitFor(t, "feedback listed", func() bool {
		c, _, body := e.do("GET", "/v1/feedback?include=decisions", nil, nil)
		if c != 200 {
			return false
		}
		m := jsonOf(t, body)
		labels, _ := m["labels"].(map[string]any)
		if labels["correct"] != float64(1) || labels["false_positive"] != float64(1) {
			return false
		}
		fb := m["feedback"].([]any)
		for _, x := range fb {
			if x.(map[string]any)["decision"] == nil {
				return false
			}
		}
		return true
	})
}
