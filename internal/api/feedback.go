package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/store"
)

var feedbackLabels = map[string]bool{"correct": true, "false_positive": true, "false_negative": true}

type feedbackBody struct {
	EventID  string `json:"event_id"`
	ActionID string `json:"action_id"`
	Label    string `json:"label"`
	Expected string `json:"expected"`
	Note     string `json:"note"`
}

func (a *API) findDecisionEvent(r *http.Request, p *auth.Principal, eventID, actionID string) (*core.Event, bool) {
	if eventID != "" {
		evs, err := a.d.Store.QueryEvents(r.Context(), store.EventFilter{Tenant: p.Tenant, IDs: []string{eventID}, Limit: 1})
		if err == nil && len(evs) == 1 && evs[0].Decision != nil {
			return &evs[0], true
		}
		return nil, false
	}
	if actionID == "" {
		return nil, false
	}
	evs, err := a.d.Store.QueryEvents(r.Context(), store.EventFilter{Tenant: p.Tenant, Limit: 5000})
	if err != nil {
		return nil, false
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Decision != nil && evs[i].Decision.ActionID == actionID {
			return &evs[i], true
		}
	}
	return nil, false
}

func (a *API) recordFeedback(w http.ResponseWriter, r *http.Request, p *auth.Principal, body feedbackBody) {
	body.Label = strings.ToLower(strings.TrimSpace(body.Label))
	if !feedbackLabels[body.Label] {
		fail(w, http.StatusBadRequest, "label must be correct, false_positive or false_negative")
		return
	}
	if body.Expected != "" && !core.ValidVerdict(core.Verdict(body.Expected)) {
		fail(w, http.StatusBadRequest, "expected must be a verdict: allow, warn, redact, hold, block or contain")
		return
	}
	if len(body.Note) > 2000 {
		body.Note = body.Note[:2000]
	}
	ev, ok := a.findDecisionEvent(r, p, body.EventID, body.ActionID)
	if !ok {
		fail(w, http.StatusNotFound, "decision event not found")
		return
	}
	detectors := make([]string, 0, len(ev.Decision.Findings))
	for _, f := range ev.Decision.Findings {
		detectors = append(detectors, f.Detector+":"+f.Rule)
	}
	fb := core.Event{
		ID:        core.NewID("evt"),
		Time:      time.Now().UTC(),
		TenantID:  ev.TenantID,
		AgentID:   ev.AgentID,
		SessionID: ev.SessionID,
		Type:      "feedback",
		Message:   body.Label + " by " + p.Name,
		Data: map[string]any{
			"event_id":  ev.ID,
			"action_id": ev.Decision.ActionID,
			"label":     body.Label,
			"expected":  body.Expected,
			"verdict":   string(ev.Decision.Verdict),
			"computed":  string(ev.Decision.Computed),
			"score":     ev.Decision.Score,
			"detectors": detectors,
			"by":        p.Name,
			"note":      body.Note,
		},
	}
	a.d.Bus.Publish(fb)
	writeJSON(w, http.StatusCreated, map[string]any{"feedback": fb})
}

func (a *API) eventFeedback(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var body feedbackBody
	if !decodeBody(w, r, &body) {
		return
	}
	body.EventID = r.PathValue("id")
	body.ActionID = ""
	a.recordFeedback(w, r, p, body)
}

func (a *API) postFeedback(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var body feedbackBody
	if !decodeBody(w, r, &body) {
		return
	}
	if body.EventID == "" && body.ActionID == "" {
		fail(w, http.StatusBadRequest, "event_id or action_id is required")
		return
	}
	a.recordFeedback(w, r, p, body)
}

func (a *API) listFeedback(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	q := r.URL.Query()
	lim := atoiDef(q.Get("limit"), 500)
	if lim > 5000 {
		lim = 5000
	}
	evs, err := a.d.Store.QueryEvents(r.Context(), store.EventFilter{Tenant: p.Tenant, AgentID: q.Get("agent"), Type: "feedback", Since: parseTime(q.Get("since")), Limit: lim})
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	counts := map[string]int{"correct": 0, "false_positive": 0, "false_negative": 0}
	for _, e := range evs {
		if l, ok := e.Data["label"].(string); ok {
			counts[l]++
		}
	}
	if q.Get("include") == "decisions" && len(evs) > 0 {
		ids := make([]string, 0, len(evs))
		for _, e := range evs {
			if id, ok := e.Data["event_id"].(string); ok {
				ids = append(ids, id)
			}
		}
		orig, err := a.d.Store.QueryEvents(r.Context(), store.EventFilter{Tenant: p.Tenant, IDs: ids, Limit: len(ids)})
		if err == nil {
			byID := make(map[string]core.Event, len(orig))
			for _, o := range orig {
				byID[o.ID] = o
			}
			for i := range evs {
				if id, ok := evs[i].Data["event_id"].(string); ok {
					if o, ok := byID[id]; ok {
						evs[i].Action = o.Action
						evs[i].Decision = o.Decision
					}
				}
			}
		}
	}
	if evs == nil {
		evs = []core.Event{}
	}
	writeJSON(w, 200, map[string]any{"feedback": evs, "count": len(evs), "labels": counts})
}
