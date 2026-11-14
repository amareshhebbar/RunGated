package mcpproxy

import (
	"encoding/json"

	"github.com/amareshhebbar/rungated/internal/jsonx"
)

type Message = map[string]any

func Parse(b []byte) (msgs []Message, batch bool, err error) {
	v, err := jsonx.DecodeAny(b)
	if err != nil {
		return nil, false, err
	}
	switch t := v.(type) {
	case map[string]any:
		return []Message{t}, false, nil
	case []any:
		for _, it := range t {
			m, ok := it.(map[string]any)
			if !ok {
				return nil, true, errInvalid
			}
			msgs = append(msgs, m)
		}
		if len(msgs) == 0 {
			return nil, true, errInvalid
		}
		return msgs, true, nil
	}
	return nil, false, errInvalid
}

type rpcError string

func (e rpcError) Error() string { return string(e) }

const errInvalid = rpcError("invalid JSON-RPC message")

func Method(m Message) string {
	s, _ := m["method"].(string)
	return s
}

func HasID(m Message) bool {
	v, ok := m["id"]
	return ok && v != nil
}

func IsRequest(m Message) bool { return Method(m) != "" && HasID(m) }

func IsNotification(m Message) bool { return Method(m) != "" && !HasID(m) }

func IsResponse(m Message) bool {
	if Method(m) != "" {
		return false
	}
	_, r := m["result"]
	_, e := m["error"]
	return HasID(m) && (r || e)
}

func IDKey(m Message) string {
	switch t := m["id"].(type) {
	case json.Number:
		return "n:" + t.String()
	case string:
		return "s:" + t
	case float64:
		b, _ := json.Marshal(t)
		return "n:" + string(b)
	}
	return ""
}

func ErrorReply(m Message, code int, msg string) Message {
	return Message{"jsonrpc": "2.0", "id": m["id"], "error": map[string]any{"code": code, "message": msg}}
}

func ToolError(m Message, text string) Message {
	return Message{"jsonrpc": "2.0", "id": m["id"], "result": map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": true,
	}}
}

func Encode(m Message) []byte { return jsonx.Marshal(m) }

func EncodeAll(ms []Message, batch bool) []byte {
	if !batch && len(ms) == 1 {
		return Encode(ms[0])
	}
	arr := make([]any, len(ms))
	for i, m := range ms {
		arr[i] = m
	}
	return jsonx.Marshal(arr)
}
