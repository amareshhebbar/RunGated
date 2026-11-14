package mcpproxy

import (
	"bytes"
	"testing"
)

func TestParseShapes(t *testing.T) {
	cases := []struct {
		in    string
		n     int
		batch bool
		bad   bool
	}{
		{`{"jsonrpc":"2.0","id":1,"method":"ping"}`, 1, false, false},
		{`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"}]`, 2, true, false},
		{`[]`, 0, true, true},
		{`[1,2]`, 0, true, true},
		{`"x"`, 0, false, true},
		{`{`, 0, false, true},
		{``, 0, false, true},
	}
	for _, c := range cases {
		ms, batch, err := Parse([]byte(c.in))
		if (err != nil) != c.bad {
			t.Fatalf("%q: err=%v", c.in, err)
		}
		if err == nil && (len(ms) != c.n || batch != c.batch) {
			t.Fatalf("%q: got %d batch=%v", c.in, len(ms), batch)
		}
	}
}

func TestMessageClassification(t *testing.T) {
	ms, _, _ := Parse([]byte(`[{"jsonrpc":"2.0","id":7,"method":"tools/list"},{"jsonrpc":"2.0","method":"notifications/x"},{"jsonrpc":"2.0","id":"a","result":{}},{"jsonrpc":"2.0","id":null,"error":{}}]`))
	if !IsRequest(ms[0]) || IsNotification(ms[0]) || IsResponse(ms[0]) {
		t.Fatal("request misclassified")
	}
	if !IsNotification(ms[1]) || IsRequest(ms[1]) {
		t.Fatal("notification misclassified")
	}
	if !IsResponse(ms[2]) || IDKey(ms[2]) != "s:a" {
		t.Fatal("response misclassified")
	}
	if IsResponse(ms[3]) {
		t.Fatal("null id error treated as correlated response")
	}
	if IDKey(ms[0]) != "n:7" {
		t.Fatalf("id key %q", IDKey(ms[0]))
	}
}

func TestReplyBuilders(t *testing.T) {
	req := Message{"id": "x"}
	e := ErrorReply(req, -32001, "nope")
	if e["id"] != "x" || e["error"].(map[string]any)["code"] != -32001 {
		t.Fatalf("%v", e)
	}
	te := ToolError(req, "blocked")
	res := te["result"].(map[string]any)
	if res["isError"] != true {
		t.Fatalf("%v", te)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	in := []byte(`[{"jsonrpc":"2.0","id":1,"method":"a"},{"jsonrpc":"2.0","id":2,"method":"b"}]`)
	ms, batch, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	out := EncodeAll(ms, batch)
	again, b2, err := Parse(out)
	if err != nil || !b2 || len(again) != 2 {
		t.Fatalf("round trip failed: %s", out)
	}
	one := EncodeAll(ms[:1], false)
	if bytes.HasPrefix(one, []byte("[")) {
		t.Fatalf("single message encoded as batch")
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"a":[1,2,{"b":null}]}}}`))
	f.Add([]byte(`[{"id":1},{"id":"2","result":{}}]`))
	f.Add([]byte(`[[]]`))
	f.Add([]byte(`{"id":1e400}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		ms, batch, err := Parse(b)
		if err != nil {
			return
		}
		for _, m := range ms {
			Method(m)
			HasID(m)
			IsRequest(m)
			IsResponse(m)
			IDKey(m)
		}
		out := EncodeAll(ms, batch)
		if _, _, err := Parse(out); err != nil {
			t.Fatalf("re-encoded message does not parse: %s", out)
		}
	})
}
