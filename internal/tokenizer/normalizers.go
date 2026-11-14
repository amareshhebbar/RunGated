package tokenizer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

type normalizer interface {
	Normalize(NString) NString
}

type jsonObj map[string]json.RawMessage

func (o jsonObj) str(k string) string {
	var s string
	json.Unmarshal(o[k], &s)
	return s
}

func (o jsonObj) boolean(k string, def bool) bool {
	v, ok := o[k]
	if !ok || string(v) == "null" {
		return def
	}
	var b bool
	if json.Unmarshal(v, &b) != nil {
		return def
	}
	return b
}

func (o jsonObj) optBool(k string) *bool {
	v, ok := o[k]
	if !ok || string(v) == "null" {
		return nil
	}
	var b bool
	if json.Unmarshal(v, &b) != nil {
		return nil
	}
	return &b
}

func (o jsonObj) num(k string, def int) int {
	v, ok := o[k]
	if !ok || string(v) == "null" {
		return def
	}
	var n int
	if json.Unmarshal(v, &n) != nil {
		return def
	}
	return n
}

func parseObj(raw json.RawMessage) (jsonObj, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var o jsonObj
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	return o, nil
}

type seqNorm []normalizer

func (s seqNorm) Normalize(n NString) NString {
	for _, x := range s {
		n = x.Normalize(n)
	}
	return n
}

type formNorm struct{ f norm.Form }

func (f formNorm) Normalize(n NString) NString { return n.Form(f.f) }

type lowerNorm struct{}

func lowerRunes(r rune) []rune {
	if r == 0x130 {
		return []rune{'i', 0x307}
	}
	return []rune{unicode.ToLower(r)}
}

func (lowerNorm) Normalize(n NString) NString { return n.Expand(lowerRunes) }

type stripAccentsNorm struct{}

func (stripAccentsNorm) Normalize(n NString) NString {
	return n.Filter(func(r rune) bool { return !unicode.Is(unicode.Mn, r) })
}

type stripNorm struct{ left, right bool }

func (s stripNorm) Normalize(n NString) NString {
	a, b := 0, n.Len()
	if s.left {
		for a < b && unicode.IsSpace(n.R[a]) {
			a++
		}
	}
	if s.right {
		for b > a && unicode.IsSpace(n.R[b-1]) {
			b--
		}
	}
	return n.Slice(a, b)
}

type prependNorm struct{ s string }

func (p prependNorm) Normalize(n NString) NString { return n.Prepend(p.s) }

type precompiledNorm struct{ c *charsMap }

func (p precompiledNorm) Normalize(n NString) NString { return p.c.normalize(n) }

type replaceNorm struct {
	str     string
	re      *regexp.Regexp
	content string
}

func (r replaceNorm) Normalize(n NString) NString {
	s := n.String()
	var spans [][2]int
	if r.re != nil {
		for _, m := range r.re.FindAllStringIndex(s, -1) {
			if m[1] > m[0] {
				spans = append(spans, [2]int{m[0], m[1]})
			}
		}
	} else if r.str != "" {
		off := 0
		for {
			i := strings.Index(s[off:], r.str)
			if i < 0 {
				break
			}
			spans = append(spans, [2]int{off + i, off + i + len(r.str)})
			off += i + len(r.str)
		}
	}
	if len(spans) == 0 {
		return n
	}
	byteToRune := make([]int, len(s)+1)
	bi := 0
	for ri, ru := range n.R {
		w := len(string(ru))
		for k := 0; k < w; k++ {
			byteToRune[bi+k] = ri
		}
		bi += w
	}
	byteToRune[len(s)] = n.Len()
	out := NString{}
	pos := 0
	for _, sp := range spans {
		a, b := byteToRune[sp[0]], byteToRune[sp[1]]
		for i := pos; i < a; i++ {
			out.push(n.R[i], n.S[i], n.E[i])
		}
		if b > a {
			for _, c := range r.content {
				out.push(c, n.S[b-1], n.E[b-1])
			}
		}
		pos = b
	}
	for i := pos; i < n.Len(); i++ {
		out.push(n.R[i], n.S[i], n.E[i])
	}
	return out
}

type bertNorm struct {
	clean, chinese bool
	strip          *bool
	lower          bool
}

func isChineseChar(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x20000 && r <= 0x2A6DF) || (r >= 0x2A700 && r <= 0x2B73F) || (r >= 0x2B740 && r <= 0x2B81F) || (r >= 0x2B920 && r <= 0x2CEAF) || (r >= 0xF900 && r <= 0xFAFF) || (r >= 0x2F800 && r <= 0x2FA1F)
}

func isBertWhitespace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' || unicode.IsSpace(r)
}

func isBertControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
}

func (b bertNorm) Normalize(n NString) NString {
	if b.clean {
		out := NString{}
		for i, r := range n.R {
			if r == 0 || r == 0xfffd || isBertControl(r) {
				continue
			}
			if isBertWhitespace(r) {
				r = ' '
			}
			out.push(r, n.S[i], n.E[i])
		}
		n = out
	}
	if b.chinese {
		out := NString{}
		for i, r := range n.R {
			if isChineseChar(r) {
				out.push(' ', n.S[i], n.S[i])
				out.push(r, n.S[i], n.E[i])
				out.push(' ', n.E[i], n.E[i])
			} else {
				out.push(r, n.S[i], n.E[i])
			}
		}
		n = out
	}
	strip := b.lower
	if b.strip != nil {
		strip = *b.strip
	}
	if strip {
		n = n.Form(norm.NFD)
		n = stripAccentsNorm{}.Normalize(n)
	}
	if b.lower {
		n = n.Expand(lowerRunes)
	}
	return n
}

func parsePattern(raw json.RawMessage) (str string, re *regexp.Regexp, err error) {
	o, err := parseObj(raw)
	if err != nil || o == nil {
		return "", nil, err
	}
	if v, ok := o["String"]; ok {
		json.Unmarshal(v, &str)
		return str, nil, nil
	}
	if v, ok := o["Regex"]; ok {
		var p string
		json.Unmarshal(v, &p)
		re, err = regexp.Compile(p)
		return "", re, err
	}
	return "", nil, fmt.Errorf("tokenizer: unknown pattern %s", string(raw))
}

func parseNormalizer(raw json.RawMessage) (normalizer, error) {
	o, err := parseObj(raw)
	if err != nil || o == nil {
		return nil, err
	}
	switch t := o.str("type"); t {
	case "Sequence":
		var items []json.RawMessage
		if err := json.Unmarshal(o["normalizers"], &items); err != nil {
			return nil, err
		}
		var seq seqNorm
		for _, it := range items {
			n, err := parseNormalizer(it)
			if err != nil {
				return nil, err
			}
			if n != nil {
				seq = append(seq, n)
			}
		}
		return seq, nil
	case "BertNormalizer":
		return bertNorm{clean: o.boolean("clean_text", true), chinese: o.boolean("handle_chinese_chars", true), strip: o.optBool("strip_accents"), lower: o.boolean("lowercase", true)}, nil
	case "Lowercase":
		return lowerNorm{}, nil
	case "NFC":
		return formNorm{norm.NFC}, nil
	case "NFD":
		return formNorm{norm.NFD}, nil
	case "NFKC":
		return formNorm{norm.NFKC}, nil
	case "NFKD":
		return formNorm{norm.NFKD}, nil
	case "StripAccents":
		return stripAccentsNorm{}, nil
	case "Strip":
		return stripNorm{left: o.boolean("strip_left", true), right: o.boolean("strip_right", true)}, nil
	case "Prepend":
		return prependNorm{o.str("prepend")}, nil
	case "Precompiled":
		c, err := newCharsMap(o.str("precompiled_charsmap"))
		if err != nil {
			return nil, err
		}
		return precompiledNorm{c}, nil
	case "Replace":
		s, re, err := parsePattern(o["pattern"])
		if err != nil {
			return nil, err
		}
		return replaceNorm{str: s, re: re, content: o.str("content")}, nil
	default:
		return nil, fmt.Errorf("tokenizer: unsupported normalizer %q", t)
	}
}
