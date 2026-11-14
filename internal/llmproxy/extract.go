package llmproxy

import (
	"github.com/amareshhebbar/rungated/internal/core"
)

type API string

const (
	APIChat      API = "openai.chat"
	APIResponses API = "openai.responses"
	APIMessages  API = "anthropic.messages"
)

func roleSource(role string) core.Source {
	switch role {
	case "system", "developer":
		return core.SourceSystem
	case "user":
		return core.SourceUser
	case "assistant", "model":
		return core.SourceAssistant
	case "tool", "function":
		return core.SourceToolResult
	}
	return core.SourceUser
}

func addText(out *[]core.TextPart, src core.Source, text, path string) {
	if text == "" {
		return
	}
	*out = append(*out, core.TextPart{Source: src, Text: text, Path: path})
}

func toolDescs(out *[]core.TextPart, body map[string]any) {
	for i, t := range arr(body["tools"]) {
		tm := obj(t)
		if fn := obj(tm["function"]); fn != nil {
			addText(out, core.SourceToolDesc, str(fn, "description"), p("tools", i, "function", "description"))
		} else {
			addText(out, core.SourceToolDesc, str(tm, "description"), p("tools", i, "description"))
		}
	}
}

func ExtractRequest(api API, body map[string]any) []core.TextPart {
	var out []core.TextPart
	switch api {
	case APIChat:
		for i, mv := range arr(body["messages"]) {
			m := obj(mv)
			src := roleSource(str(m, "role"))
			switch c := m["content"].(type) {
			case string:
				addText(&out, src, c, p("messages", i, "content"))
			case []any:
				for k, part := range c {
					pm := obj(part)
					if t := str(pm, "type"); t == "text" || t == "input_text" {
						addText(&out, src, str(pm, "text"), p("messages", i, "content", k, "text"))
					}
				}
			}
		}
	case APIResponses:
		addText(&out, core.SourceSystem, str(body, "instructions"), "instructions")
		switch in := body["input"].(type) {
		case string:
			addText(&out, core.SourceUser, in, "input")
		case []any:
			for i, it := range in {
				m := obj(it)
				typ := str(m, "type")
				switch {
				case typ == "function_call_output" || typ == "custom_tool_call_output" || typ == "mcp_call_output":
					if s, ok := m["output"].(string); ok {
						addText(&out, core.SourceToolResult, s, p("input", i, "output"))
					} else {
						for k, part := range arr(m["output"]) {
							addText(&out, core.SourceToolResult, str(obj(part), "text"), p("input", i, "output", k, "text"))
						}
					}
				case typ == "message" || (typ == "" && m["role"] != nil):
					src := roleSource(str(m, "role"))
					switch c := m["content"].(type) {
					case string:
						addText(&out, src, c, p("input", i, "content"))
					case []any:
						for k, part := range c {
							pm := obj(part)
							switch str(pm, "type") {
							case "input_text", "output_text", "text":
								addText(&out, src, str(pm, "text"), p("input", i, "content", k, "text"))
							}
						}
					}
				}
			}
		}
	case APIMessages:
		switch s := body["system"].(type) {
		case string:
			addText(&out, core.SourceSystem, s, "system")
		case []any:
			for k, b := range s {
				addText(&out, core.SourceSystem, str(obj(b), "text"), p("system", k, "text"))
			}
		}
		for i, mv := range arr(body["messages"]) {
			m := obj(mv)
			src := roleSource(str(m, "role"))
			switch c := m["content"].(type) {
			case string:
				addText(&out, src, c, p("messages", i, "content"))
			case []any:
				for k, bv := range c {
					b := obj(bv)
					switch str(b, "type") {
					case "text":
						addText(&out, src, str(b, "text"), p("messages", i, "content", k, "text"))
					case "tool_result":
						switch tc := b["content"].(type) {
						case string:
							addText(&out, core.SourceToolResult, tc, p("messages", i, "content", k, "content"))
						case []any:
							for j, x := range tc {
								xm := obj(x)
								if str(xm, "type") == "text" {
									addText(&out, core.SourceToolResult, str(xm, "text"), p("messages", i, "content", k, "content", j, "text"))
								}
							}
						}
					case "document":
						if src := obj(b["source"]); src != nil && str(src, "type") == "text" {
							addText(&out, core.SourceFile, str(src, "data"), p("messages", i, "content", k, "source", "data"))
						}
					}
				}
			}
		}
	}
	toolDescs(&out, body)
	return out
}

func MaxTokens(api API, body map[string]any) int {
	for _, k := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if n := num(body[k]); n > 0 {
			return n
		}
	}
	return 0
}

type ToolCall struct {
	ID       string
	Name     string
	ArgsText string
	Args     map[string]any
	Index    int
	Path     string
}

type Parsed struct {
	Texts     []core.TextPart
	ToolCalls []ToolCall
	InTokens  int
	OutTokens int
}

func ParseResponse(api API, body map[string]any) Parsed {
	var pr Parsed
	switch api {
	case APIChat:
		for i, cv := range arr(body["choices"]) {
			msg := obj(obj(cv)["message"])
			addText(&pr.Texts, core.SourceAssistant, str(msg, "content"), p("choices", i, "message", "content"))
			for j, tv := range arr(msg["tool_calls"]) {
				t := obj(tv)
				fn := obj(t["function"])
				args := str(fn, "arguments")
				pr.ToolCalls = append(pr.ToolCalls, ToolCall{ID: str(t, "id"), Name: str(fn, "name"), ArgsText: args, Args: parseArgs(args), Index: j, Path: p("choices", i, "message", "tool_calls", j)})
			}
		}
		u := obj(body["usage"])
		pr.InTokens, pr.OutTokens = num(u["prompt_tokens"]), num(u["completion_tokens"])
	case APIResponses:
		for i, iv := range arr(body["output"]) {
			it := obj(iv)
			switch str(it, "type") {
			case "message":
				for k, cv := range arr(it["content"]) {
					c := obj(cv)
					if str(c, "type") == "output_text" {
						addText(&pr.Texts, core.SourceAssistant, str(c, "text"), p("output", i, "content", k, "text"))
					}
				}
			case "function_call":
				args := str(it, "arguments")
				pr.ToolCalls = append(pr.ToolCalls, ToolCall{ID: str(it, "call_id"), Name: str(it, "name"), ArgsText: args, Args: parseArgs(args), Index: i, Path: p("output", i)})
			}
		}
		u := obj(body["usage"])
		pr.InTokens, pr.OutTokens = num(u["input_tokens"]), num(u["output_tokens"])
	case APIMessages:
		for i, bv := range arr(body["content"]) {
			b := obj(bv)
			switch str(b, "type") {
			case "text":
				addText(&pr.Texts, core.SourceAssistant, str(b, "text"), p("content", i, "text"))
			case "tool_use":
				in := obj(b["input"])
				if in == nil {
					in = map[string]any{}
				}
				pr.ToolCalls = append(pr.ToolCalls, ToolCall{ID: str(b, "id"), Name: str(b, "name"), ArgsText: string(marshal(in)), Args: in, Index: i, Path: p("content", i)})
			}
		}
		u := obj(body["usage"])
		pr.InTokens, pr.OutTokens = num(u["input_tokens"]), num(u["output_tokens"])
	}
	return pr
}
