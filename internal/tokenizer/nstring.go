package tokenizer

import (
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type NString struct {
	R []rune
	S []int
	E []int
}

func NewNString(s string) NString {
	n := NString{R: make([]rune, 0, len(s)), S: make([]int, 0, len(s)), E: make([]int, 0, len(s))}
	for i, r := range s {
		w := utf8.RuneLen(r)
		if r == utf8.RuneError {
			w = 1
		}
		n.R = append(n.R, r)
		n.S = append(n.S, i)
		n.E = append(n.E, i+w)
	}
	return n
}

func (n NString) Len() int { return len(n.R) }

func (n NString) String() string { return string(n.R) }

func (n NString) Slice(a, b int) NString {
	return NString{R: n.R[a:b], S: n.S[a:b], E: n.E[a:b]}
}

func (n *NString) push(r rune, s, e int) {
	n.R = append(n.R, r)
	n.S = append(n.S, s)
	n.E = append(n.E, e)
}

func (n NString) Filter(keep func(rune) bool) NString {
	out := NString{}
	for i, r := range n.R {
		if keep(r) {
			out.push(r, n.S[i], n.E[i])
		}
	}
	return out
}

func (n NString) Expand(f func(r rune) []rune) NString {
	out := NString{}
	for i, r := range n.R {
		for _, x := range f(r) {
			out.push(x, n.S[i], n.E[i])
		}
	}
	return out
}

func (n NString) Prepend(prefix string) NString {
	if n.Len() == 0 {
		return n
	}
	out := NString{}
	for _, r := range prefix {
		out.push(r, n.S[0], n.E[0])
	}
	out.R = append(out.R, n.R...)
	out.S = append(out.S, n.S...)
	out.E = append(out.E, n.E...)
	return out
}

func (n NString) Concat(o NString) NString {
	out := NString{R: append(append([]rune{}, n.R...), o.R...), S: append(append([]int{}, n.S...), o.S...), E: append(append([]int{}, n.E...), o.E...)}
	return out
}

func (n NString) replaceInto(out *NString, i, j int, repl []rune) {
	for k, r := range repl {
		if k < j-i {
			out.push(r, n.S[i+k], n.E[i+k])
		} else {
			last := out.Len() - 1
			out.push(r, out.S[last], out.E[last])
		}
	}
}

func (n NString) Form(f norm.Form) NString {
	s := string(n.R)
	if f.IsNormalString(s) {
		return n
	}
	out := NString{}
	pos := 0
	runeIdx := 0
	for pos < len(s) {
		next := f.NextBoundaryInString(s[pos:], true)
		if next <= 0 {
			next = len(s) - pos
		}
		seg := s[pos : pos+next]
		cnt := utf8.RuneCountInString(seg)
		whole := f.String(seg)
		if cnt > 1 && (f == norm.NFD || f == norm.NFKD) {
			var per []rune
			var spans [][2]int
			for k, r := range []rune(seg) {
				d := []rune(f.String(string(r)))
				per = append(per, d...)
				for range d {
					spans = append(spans, [2]int{n.S[runeIdx+k], n.E[runeIdx+k]})
				}
			}
			if string(per) == whole {
				for k, r := range per {
					out.push(r, spans[k][0], spans[k][1])
				}
				pos += next
				runeIdx += cnt
				continue
			}
		}
		n.replaceInto(&out, runeIdx, runeIdx+cnt, []rune(whole))
		pos += next
		runeIdx += cnt
	}
	return out
}

type span struct {
	a, b  int
	match bool
}

func (n NString) splitSpans(spans []span, behavior string) []NString {
	type seg struct {
		a, b  int
		match bool
	}
	var segs []seg
	switch behavior {
	case "removed":
		for _, s := range spans {
			segs = append(segs, seg{s.a, s.b, s.match})
		}
	case "isolated":
		for _, s := range spans {
			segs = append(segs, seg{s.a, s.b, false})
		}
	case "merged_with_previous":
		prev := false
		for _, s := range spans {
			if s.match && !prev {
				if len(segs) > 0 {
					segs[len(segs)-1].b = s.b
				} else {
					segs = append(segs, seg{s.a, s.b, false})
				}
			} else {
				segs = append(segs, seg{s.a, s.b, false})
			}
			prev = s.match
		}
	case "merged_with_next":
		var rev []seg
		prev := false
		for i := len(spans) - 1; i >= 0; i-- {
			s := spans[i]
			if s.match && !prev {
				if len(rev) > 0 {
					rev[len(rev)-1].a = s.a
				} else {
					rev = append(rev, seg{s.a, s.b, false})
				}
			} else {
				rev = append(rev, seg{s.a, s.b, false})
			}
			prev = s.match
		}
		for i := len(rev) - 1; i >= 0; i-- {
			segs = append(segs, rev[i])
		}
	case "contiguous":
		prev := false
		for _, s := range spans {
			if s.match == prev {
				if len(segs) > 0 {
					segs[len(segs)-1].b = s.b
				} else {
					segs = append(segs, seg{s.a, s.b, false})
				}
			} else {
				segs = append(segs, seg{s.a, s.b, false})
			}
			prev = s.match
		}
	default:
		for _, s := range spans {
			segs = append(segs, seg{s.a, s.b, false})
		}
	}
	var out []NString
	for _, s := range segs {
		if s.match || s.a >= s.b {
			continue
		}
		out = append(out, n.Slice(s.a, s.b))
	}
	return out
}

func (n NString) SplitFunc(isMatch func(rune) bool, behavior string, invert bool) []NString {
	var spans []span
	i := 0
	for i < n.Len() {
		m := isMatch(n.R[i]) != invert
		if m {
			spans = append(spans, span{i, i + 1, true})
			i++
			continue
		}
		j := i + 1
		for j < n.Len() && (isMatch(n.R[j]) == invert) {
			j++
		}
		spans = append(spans, span{i, j, false})
		i = j
	}
	return n.splitSpans(spans, behavior)
}

func (n NString) SplitSpans(spans []span, behavior string) []NString {
	return n.splitSpans(spans, behavior)
}

func (n NString) Origin() (int, int) {
	if n.Len() == 0 {
		return 0, 0
	}
	return n.S[0], n.E[n.Len()-1]
}
