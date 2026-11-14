package llmproxy

import (
	"testing"
)

func TestExtractPaths(t *testing.T) {
	body, _ := decode([]byte(`{"messages":[{"role":"user","content":"hi"},{"role":"tool","content":[{"type":"text","text":"result"}]}],"tools":[{"type":"function","function":{"description":"desc"}}]}`))
	parts := ExtractRequest(APIChat, body)
	if len(parts) != 3 {
		t.Fatalf("parts=%v", parts)
	}
	for _, p := range parts {
		if p.Path == "" {
			t.Fatalf("missing path: %+v", p)
		}
	}
	if !setPath(body, parts[1].Path, "changed") {
		t.Fatalf("setPath failed for %s", parts[1].Path)
	}
	if ExtractRequest(APIChat, body)[1].Text != "changed" {
		t.Fatal("setPath did not write through")
	}
}

func FuzzExtractRequest(f *testing.F) {
	f.Add(`{"messages":[{"role":"user","content":"x"}]}`)
	f.Add(`{"input":[{"type":"function_call_output","output":"x"}],"instructions":"y"}`)
	f.Add(`{"system":[{"text":"a"}],"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"z"}]}]}]}`)
	f.Fuzz(func(t *testing.T, in string) {
		body, err := decode([]byte(in))
		if err != nil {
			return
		}
		for _, api := range []API{APIChat, APIResponses, APIMessages} {
			parts := ExtractRequest(api, body)
			for _, p := range parts {
				setPath(body, p.Path, p.Text)
			}
			MaxTokens(api, body)
			ParseResponse(api, body)
		}
	})
}
