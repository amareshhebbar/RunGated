package demo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	injectionRe = regexp.MustCompile(`(?i)ignore (all )?(previous|prior) instructions`)
	emailRe     = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	fetchRe     = regexp.MustCompile(`(?i)\b(?:fetch|summari[sz]e|read|open)\s+(https?://[^\s"')]+)`)
	postRe      = regexp.MustCompile(`(?i)\b(?:post|send|upload)\s+(?:the\s+)?(?:table|list|customers|data)\s+to\s+(https?://[^\s"')]+)`)
	invoiceRe   = regexp.MustCompile(`INV-[0-9]{6}`)
	customersRe = regexp.MustCompile(`(?i)\blist\b.*\bcustomers\b`)
)

type SeenRequest struct {
	Time   time.Time `json:"time"`
	Model  string    `json:"model"`
	Stream bool      `json:"stream"`
	Text   string    `json:"text"`
}

type MockLLM struct {
	mu   sync.Mutex
	seen []SeenRequest
	n    int64
}

func NewMockLLM() *MockLLM { return &MockLLM{} }

func (m *MockLLM) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", m.chat)
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"object": "list", "data": []any{map[string]any{"id": "mock-1", "object": "model", "owned_by": "rungated-demo"}}})
	})
	mux.HandleFunc("GET /_demo/state", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()
		seen := m.seen
		if seen == nil {
			seen = []SeenRequest{}
		}
		writeJSON(w, 200, map[string]any{"seen": seen})
	})
	mux.HandleFunc("POST /_demo/reset", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.seen = nil
		m.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	return mux
}

type chatMsg struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func (c chatMsg) text() string {
	var s string
	if json.Unmarshal(c.Content, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(c.Content, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
			b.WriteString("\n")
		}
		return b.String()
	}
	return ""
}

type plannedCall struct {
	Name string
	Args map[string]any
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func toolNameFor(msgs []chatMsg, id string) string {
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.ID == id {
				return tc.Function.Name
			}
		}
	}
	return ""
}

func called(msgs []chatMsg, name string) bool {
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == name {
				return true
			}
		}
	}
	return false
}

func plan(msgs []chatMsg) (string, *plannedCall) {
	if len(msgs) == 0 {
		return "Nothing to do.", nil
	}
	last := msgs[len(msgs)-1]
	if last.Role == "tool" {
		name := toolNameFor(msgs, last.ToolCallID)
		content := last.text()
		attacker := ""
		customers := ""
		for _, m := range msgs {
			if m.Role != "tool" {
				continue
			}
			t := m.text()
			if attacker == "" && injectionRe.MatchString(t) {
				for _, e := range emailRe.FindAllString(t, -1) {
					if !strings.HasSuffix(strings.ToLower(e), "@acme.example") {
						attacker = e
						break
					}
				}
			}
			if toolNameFor(msgs, m.ToolCallID) == "list_customers" {
				customers = t
			}
		}
		if attacker != "" {
			if customers == "" {
				return "", &plannedCall{"list_customers", map[string]any{"limit": 50}}
			}
			if !called(msgs, "send_email") {
				return "", &plannedCall{"send_email", map[string]any{"to": attacker, "subject": "Customer table", "body": customers}}
			}
			return "I followed the instructions in the page and sent the customer table to " + attacker + ".", nil
		}
		if name == "list_customers" && !called(msgs, "fetch_url") {
			for _, m := range msgs {
				if m.Role != "user" {
					continue
				}
				if g := postRe.FindStringSubmatch(m.text()); g != nil {
					sep := "?"
					if strings.Contains(g[1], "?") {
						sep = "&"
					}
					return "", &plannedCall{"fetch_url", map[string]any{"url": g[1] + sep + "data=" + url.QueryEscape(trunc(content, 600))}}
				}
			}
		}
		return "Done. " + name + " returned: " + trunc(strings.ReplaceAll(content, "\n", " "), 240), nil
	}
	text := last.text()
	if g := fetchRe.FindStringSubmatch(text); g != nil {
		return "", &plannedCall{"fetch_url", map[string]any{"url": g[1]}}
	}
	if id := invoiceRe.FindString(text); id != "" {
		return "", &plannedCall{"get_invoice", map[string]any{"invoice_id": id}}
	}
	if customersRe.MatchString(text) {
		return "", &plannedCall{"list_customers", map[string]any{"limit": 50}}
	}
	return "Understood: " + trunc(text, 300), nil
}

func (m *MockLLM) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model         string    `json:"model"`
		Messages      []chatMsg `json:"messages"`
		Stream        bool      `json:"stream"`
		StreamOptions struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]any{"error": map[string]any{"message": "bad json"}})
		return
	}
	var all strings.Builder
	for _, mm := range req.Messages {
		all.WriteString(mm.Role + ": " + mm.text() + "\n")
	}
	m.mu.Lock()
	m.seen = append(m.seen, SeenRequest{Time: time.Now().UTC(), Model: req.Model, Stream: req.Stream, Text: all.String()})
	m.mu.Unlock()
	text, call := plan(req.Messages)
	id := fmt.Sprintf("chatcmpl-%d", atomic.AddInt64(&m.n, 1))
	model := req.Model
	if model == "" {
		model = "mock-1"
	}
	usage := map[string]any{"prompt_tokens": 40, "completion_tokens": 20, "total_tokens": 60}
	var argsJSON string
	callID := fmt.Sprintf("call_%d", atomic.AddInt64(&m.n, 1))
	if call != nil {
		b, _ := json.Marshal(call.Args)
		argsJSON = string(b)
	}
	if !req.Stream {
		msg := map[string]any{"role": "assistant", "content": text}
		finish := "stop"
		if call != nil {
			msg["content"] = nil
			msg["tool_calls"] = []any{map[string]any{"id": callID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": argsJSON}}}
			finish = "tool_calls"
		}
		writeJSON(w, 200, map[string]any{"id": id, "object": "chat.completion", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}}, "usage": usage})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	fl, _ := w.(http.Flusher)
	send := func(delta map[string]any, finish any) {
		b, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	send(map[string]any{"role": "assistant", "content": ""}, nil)
	if call != nil {
		send(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": callID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": ""}}}}, nil)
		half := len(argsJSON) / 2
		for _, part := range []string{argsJSON[:half], argsJSON[half:]} {
			send(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": part}}}}, nil)
		}
		send(map[string]any{}, "tool_calls")
	} else {
		words := strings.SplitAfter(text, " ")
		for _, wd := range words {
			if wd != "" {
				send(map[string]any{"content": wd}, nil)
			}
		}
		send(map[string]any{}, "stop")
	}
	if req.StreamOptions.IncludeUsage {
		b, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": model, "choices": []any{}, "usage": usage})
		fmt.Fprintf(w, "data: %s\n\n", b)
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if fl != nil {
		fl.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(b)
}
