package bench

type Part struct {
	Source string `json:"source"`
	Text   string `json:"text"`
}

type Case struct {
	ID       string         `json:"id"`
	Class    string         `json:"class"`
	Label    string         `json:"label"`
	Mutation string         `json:"mutation,omitempty"`
	Lang     string         `json:"lang,omitempty"`
	Kind     string         `json:"kind"`
	Parts    []Part         `json:"parts,omitempty"`
	Server   string         `json:"server,omitempty"`
	Tool     string         `json:"tool,omitempty"`
	Args     map[string]any `json:"args,omitempty"`
	Method   string         `json:"method,omitempty"`
	Host     string         `json:"host,omitempty"`
	URL      string         `json:"url,omitempty"`
}

const (
	Attack = "attack"
	Benign = "benign"
)

func (c Case) request(agent string) map[string]any {
	r := map[string]any{"agent_id": agent, "session_id": "bench-" + c.ID, "kind": c.Kind}
	if len(c.Parts) > 0 {
		r["texts"] = c.Parts
	}
	if c.Kind == "llm.request" {
		r["model"] = "mock-1"
		r["provider"] = "mock"
	}
	if c.Server != "" {
		r["server"] = c.Server
	}
	if c.Tool != "" {
		r["tool"] = c.Tool
	}
	if c.Args != nil {
		r["args"] = c.Args
	}
	if c.Method != "" {
		r["method"] = c.Method
	}
	if c.Host != "" {
		r["host"] = c.Host
	}
	if c.URL != "" {
		r["url"] = c.URL
	}
	return r
}
