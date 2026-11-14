package mcptest

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/amareshhebbar/rungated/internal/jsonx"
)

type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(args map[string]any) (string, bool)
}

type Call struct {
	Tool string
	Args map[string]any
}

type Server struct {
	mu        sync.Mutex
	tools     map[string]Tool
	order     []string
	resources map[string]string
	prompts   map[string]string
	calls     []Call
	headers   http.Header
	sessions  int
	SSE       bool
	Name      string
}

func New() *Server {
	return &Server{tools: map[string]Tool{}, resources: map[string]string{}, prompts: map[string]string{}, Name: "mcptest"}
}

func (s *Server) SetTool(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tools[t.Name]; !ok {
		s.order = append(s.order, t.Name)
	}
	if t.InputSchema == nil {
		t.InputSchema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	s.tools[t.Name] = t
}

func (s *Server) RemoveTool(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tools, name)
	for i, n := range s.order {
		if n == name {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
}

func (s *Server) SetResource(uri, text string) {
	s.mu.Lock()
	s.resources[uri] = text
	s.mu.Unlock()
}

func (s *Server) SetPrompt(name, text string) {
	s.mu.Lock()
	s.prompts[name] = text
	s.mu.Unlock()
}

func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

func (s *Server) Headers() http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.headers.Clone()
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		w.WriteHeader(200)
		return
	case http.MethodPost:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	s.headers = r.Header.Clone()
	s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	v, err := jsonx.DecodeAny(body)
	if err != nil {
		http.Error(w, "bad json", 400)
		return
	}
	var msgs []map[string]any
	batch := false
	switch t := v.(type) {
	case map[string]any:
		msgs = []map[string]any{t}
	case []any:
		batch = true
		for _, x := range t {
			if m, ok := x.(map[string]any); ok {
				msgs = append(msgs, m)
			}
		}
	}
	var out []any
	initialized := false
	for _, m := range msgs {
		method, _ := m["method"].(string)
		if method == "initialize" {
			initialized = true
		}
		if m["id"] == nil {
			continue
		}
		out = append(out, s.handle(method, m))
	}
	if initialized {
		s.mu.Lock()
		s.sessions++
		id := fmt.Sprintf("sess-%d", s.sessions)
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
	}
	if len(out) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if s.SSE {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		for _, o := range out {
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", jsonx.Marshal(o))
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if batch {
		w.Write(jsonx.Marshal(out))
	} else {
		w.Write(jsonx.Marshal(out[0]))
	}
}

func rpcErr(id any, code int, msg string) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}}
}

func (s *Server) handle(method string, m map[string]any) map[string]any {
	id := m["id"]
	params := jsonx.Obj(m["params"])
	ok := func(res any) map[string]any { return map[string]any{"jsonrpc": "2.0", "id": id, "result": res} }
	s.mu.Lock()
	defer s.mu.Unlock()
	switch method {
	case "initialize":
		return ok(map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": true}, "resources": map[string]any{}, "prompts": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": "1.0.0"},
		})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		list := make([]any, 0, len(s.order))
		for _, n := range s.order {
			t := s.tools[n]
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.InputSchema})
		}
		return ok(map[string]any{"tools": list})
	case "tools/call":
		name := jsonx.Str(params, "name")
		t, found := s.tools[name]
		if !found {
			return rpcErr(id, -32602, "unknown tool "+name)
		}
		args := jsonx.Obj(params["arguments"])
		s.calls = append(s.calls, Call{Tool: name, Args: args})
		text, isErr := "ok", false
		if t.Handler != nil {
			s.mu.Unlock()
			text, isErr = t.Handler(args)
			s.mu.Lock()
		}
		return ok(map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": isErr})
	case "resources/list":
		var uris []string
		for u := range s.resources {
			uris = append(uris, u)
		}
		sort.Strings(uris)
		list := make([]any, len(uris))
		for i, u := range uris {
			list[i] = map[string]any{"uri": u, "name": u}
		}
		return ok(map[string]any{"resources": list})
	case "resources/read":
		uri := jsonx.Str(params, "uri")
		txt, found := s.resources[uri]
		if !found {
			return rpcErr(id, -32002, "resource not found")
		}
		return ok(map[string]any{"contents": []any{map[string]any{"uri": uri, "mimeType": "text/plain", "text": txt}}})
	case "prompts/list":
		var names []string
		for n := range s.prompts {
			names = append(names, n)
		}
		sort.Strings(names)
		list := make([]any, len(names))
		for i, n := range names {
			list[i] = map[string]any{"name": n}
		}
		return ok(map[string]any{"prompts": list})
	case "prompts/get":
		name := jsonx.Str(params, "name")
		txt, found := s.prompts[name]
		if !found {
			return rpcErr(id, -32602, "prompt not found")
		}
		return ok(map[string]any{"messages": []any{map[string]any{"role": "user", "content": map[string]any{"type": "text", "text": txt}}}})
	}
	return rpcErr(id, -32601, "method not found: "+method)
}
