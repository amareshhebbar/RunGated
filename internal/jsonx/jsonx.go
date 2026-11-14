package jsonx

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

func Decode(b []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func DecodeAny(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}

func Marshal(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
	return bytes.TrimRight(buf.Bytes(), "\n")
}

func SetPath(root any, path string, val any) bool {
	parts := strings.Split(path, ".")
	cur := root
	for i, p := range parts {
		last := i == len(parts)-1
		switch c := cur.(type) {
		case map[string]any:
			if last {
				c[p] = val
				return true
			}
			cur = c[p]
		case []any:
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 || n >= len(c) {
				return false
			}
			if last {
				c[n] = val
				return true
			}
			cur = c[n]
		default:
			return false
		}
	}
	return false
}

func Str(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func Obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func Arr(v any) []any {
	a, _ := v.([]any)
	return a
}

func Num(v any) int {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case float64:
		return int(x)
	case int:
		return x
	}
	return 0
}

func Path(parts ...any) string {
	s := make([]string, len(parts))
	for i, x := range parts {
		switch v := x.(type) {
		case string:
			s[i] = v
		case int:
			s[i] = strconv.Itoa(v)
		}
	}
	return strings.Join(s, ".")
}
