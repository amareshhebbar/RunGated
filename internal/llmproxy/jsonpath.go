package llmproxy

import (
	"github.com/amareshhebbar/rungated/internal/jsonx"
)

func decode(b []byte) (map[string]any, error)     { return jsonx.Decode(b) }
func decodeAny(b []byte) (any, error)             { return jsonx.DecodeAny(b) }
func marshal(v any) []byte                        { return jsonx.Marshal(v) }
func setPath(root any, path string, val any) bool { return jsonx.SetPath(root, path, val) }
func str(m map[string]any, k string) string       { return jsonx.Str(m, k) }
func obj(v any) map[string]any                    { return jsonx.Obj(v) }
func arr(v any) []any                             { return jsonx.Arr(v) }
func num(v any) int                               { return jsonx.Num(v) }
func p(parts ...any) string                       { return jsonx.Path(parts...) }

func parseArgs(s string) map[string]any {
	if len(s) == 0 {
		return map[string]any{}
	}
	trim := true
	for _, c := range s {
		if c != ' ' && c != '\n' && c != '\t' && c != '\r' {
			trim = false
			break
		}
	}
	if trim {
		return map[string]any{}
	}
	v, err := decodeAny([]byte(s))
	if err != nil {
		return map[string]any{"_raw": s}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"_value": v}
}
