package tokenizer

import (
	"encoding/json"
	"fmt"
	"regexp"
	"unicode"
)

type preTokenizer interface {
	Pre(pieces []NString) []NString
}

type seqPre []preTokenizer

func (s seqPre) Pre(p []NString) []NString {
	for _, x := range s {
		p = x.Pre(p)
	}
	return p
}

func applySplit(pieces []NString, f func(NString) []NString) []NString {
	var out []NString
	for _, p := range pieces {
		out = append(out, f(p)...)
	}
	return out
}

type bertPre struct{}

func isBertPunct(r rune) bool {
	if r < 128 {
		return (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126)
	}
	return unicode.IsPunct(r)
}

func (bertPre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString {
		var out []NString
		for _, w := range p.SplitFunc(unicode.IsSpace, "removed", false) {
			out = append(out, w.SplitFunc(isBertPunct, "isolated", false)...)
		}
		return out
	})
}

type whitespaceSplitPre struct{}

func (whitespaceSplitPre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString { return p.SplitFunc(unicode.IsSpace, "removed", false) })
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.Is(unicode.Nd, r) || unicode.Is(unicode.Nl, r) || unicode.Is(unicode.Pc, r) || r == 0x200c || r == 0x200d
}

type whitespacePre struct{}

func (whitespacePre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString {
		var out []NString
		i := 0
		for i < p.Len() {
			r := p.R[i]
			if unicode.IsSpace(r) {
				i++
				continue
			}
			j := i + 1
			if isWordRune(r) {
				for j < p.Len() && isWordRune(p.R[j]) {
					j++
				}
			} else {
				for j < p.Len() && !isWordRune(p.R[j]) && !unicode.IsSpace(p.R[j]) {
					j++
				}
			}
			out = append(out, p.Slice(i, j))
			i = j
		}
		return out
	})
}

type punctuationPre struct{ behavior string }

func (b punctuationPre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString { return p.SplitFunc(isBertPunct, b.behavior, false) })
}

type digitsPre struct{ individual bool }

func (d digitsPre) Pre(pieces []NString) []NString {
	beh := "contiguous"
	if d.individual {
		beh = "isolated"
	}
	return applySplit(pieces, func(p NString) []NString {
		return p.SplitFunc(func(r rune) bool { return unicode.IsDigit(r) }, beh, false)
	})
}

func behaviorName(s string) string {
	switch s {
	case "Removed":
		return "removed"
	case "Isolated":
		return "isolated"
	case "MergedWithPrevious":
		return "merged_with_previous"
	case "MergedWithNext":
		return "merged_with_next"
	case "Contiguous":
		return "contiguous"
	}
	return "isolated"
}

type splitPre struct {
	str      string
	re       *regexp.Regexp
	behavior string
	invert   bool
}

func (s splitPre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString {
		str := p.String()
		byteToRune := make([]int, len(str)+1)
		bi := 0
		for ri, ru := range p.R {
			w := len(string(ru))
			for k := 0; k < w; k++ {
				byteToRune[bi+k] = ri
			}
			bi += w
		}
		byteToRune[len(str)] = p.Len()
		var matches [][2]int
		if s.re != nil {
			for _, m := range s.re.FindAllStringIndex(str, -1) {
				if m[1] > m[0] {
					matches = append(matches, [2]int{byteToRune[m[0]], byteToRune[m[1]]})
				}
			}
		} else if s.str != "" {
			off := 0
			for off <= len(str) {
				i := indexFrom(str, s.str, off)
				if i < 0 {
					break
				}
				matches = append(matches, [2]int{byteToRune[i], byteToRune[i+len(s.str)]})
				off = i + len(s.str)
			}
		}
		var spans []span
		pos := 0
		for _, m := range matches {
			if m[0] > pos {
				spans = append(spans, span{pos, m[0], s.invert})
			}
			spans = append(spans, span{m[0], m[1], !s.invert})
			pos = m[1]
		}
		if pos < p.Len() {
			spans = append(spans, span{pos, p.Len(), s.invert})
		}
		return p.SplitSpans(spans, s.behavior)
	})
}

func indexFrom(s, sub string, from int) int {
	if from > len(s) {
		return -1
	}
	for i := from; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type metaspacePre struct {
	repl   string
	scheme string
	split  bool
}

func (m metaspacePre) Pre(pieces []NString) []NString {
	var out []NString
	for idx, p := range pieces {
		replaced := p.Expand(func(r rune) []rune {
			if r == ' ' {
				return []rune(m.repl)
			}
			return []rune{r}
		})
		first := ""
		if replaced.Len() > 0 {
			first = string(replaced.R[0])
		}
		if m.scheme != "never" && first != m.repl {
			if m.scheme == "always" || (m.scheme == "first" && idx == 0) {
				replaced = replaced.Prepend(m.repl)
			}
		}
		if m.split {
			r0 := []rune(m.repl)[0]
			out = append(out, replaced.SplitFunc(func(r rune) bool { return r == r0 }, "merged_with_next", false)...)
		} else {
			out = append(out, replaced)
		}
	}
	return out
}

var byteToUnicode [256]rune

func init() {
	var bs []int
	for i := '!'; i <= '~'; i++ {
		bs = append(bs, int(i))
	}
	for i := 0xA1; i <= 0xAC; i++ {
		bs = append(bs, i)
	}
	for i := 0xAE; i <= 0xFF; i++ {
		bs = append(bs, i)
	}
	have := map[int]bool{}
	for _, b := range bs {
		have[b] = true
		byteToUnicode[b] = rune(b)
	}
	n := 0
	for b := 0; b < 256; b++ {
		if !have[b] {
			byteToUnicode[b] = rune(256 + n)
			n++
		}
	}
}

type byteLevelPre struct {
	addPrefixSpace bool
	useRegex       bool
}

func isLetterR(r rune) bool { return unicode.IsLetter(r) }
func isNumberR(r rune) bool { return unicode.IsNumber(r) }

func gpt2Split(rs []rune) [][2]int {
	var out [][2]int
	i := 0
	n := len(rs)
	hasPrefix := func(i int, s string) bool {
		rr := []rune(s)
		if i+len(rr) > n {
			return false
		}
		for k, c := range rr {
			if rs[i+k] != c {
				return false
			}
		}
		return true
	}
	for i < n {
		matched := false
		if rs[i] == '\'' {
			for _, c := range []string{"'s", "'t", "'re", "'ve", "'m", "'ll", "'d"} {
				if hasPrefix(i, c) {
					out = append(out, [2]int{i, i + len([]rune(c))})
					i += len([]rune(c))
					matched = true
					break
				}
			}
			if matched {
				continue
			}
		}
		try := func(pred func(rune) bool) bool {
			j := i
			if rs[j] == ' ' {
				j++
			}
			if j < n && pred(rs[j]) {
				k := j
				for k < n && pred(rs[k]) {
					k++
				}
				out = append(out, [2]int{i, k})
				i = k
				return true
			}
			return false
		}
		if try(isLetterR) || try(isNumberR) || try(func(r rune) bool { return !unicode.IsSpace(r) && !isLetterR(r) && !isNumberR(r) }) {
			continue
		}
		if unicode.IsSpace(rs[i]) {
			k := i
			for k < n && unicode.IsSpace(rs[k]) {
				k++
			}
			if k < n && k-i > 1 {
				out = append(out, [2]int{i, k - 1})
				i = k - 1
				continue
			}
			if k == n {
				out = append(out, [2]int{i, k})
				i = k
				continue
			}
			out = append(out, [2]int{i, k})
			i = k
			continue
		}
		out = append(out, [2]int{i, i + 1})
		i++
	}
	return out
}

func (b byteLevelPre) Pre(pieces []NString) []NString {
	return applySplit(pieces, func(p NString) []NString {
		if b.addPrefixSpace && (p.Len() == 0 || p.R[0] != ' ') {
			p = p.Prepend(" ")
		}
		var parts []NString
		if b.useRegex {
			for _, sp := range gpt2Split(p.R) {
				parts = append(parts, p.Slice(sp[0], sp[1]))
			}
		} else {
			parts = []NString{p}
		}
		var out []NString
		for _, part := range parts {
			m := NString{}
			for i, r := range part.R {
				var buf [4]byte
				l := encodeRune(buf[:], r)
				for _, by := range buf[:l] {
					m.push(byteToUnicode[by], part.S[i], part.E[i])
				}
			}
			out = append(out, m)
		}
		return out
	})
}

func encodeRune(buf []byte, r rune) int {
	s := string(r)
	return copy(buf, s)
}

func parsePreTokenizer(raw json.RawMessage) (preTokenizer, error) {
	o, err := parseObj(raw)
	if err != nil || o == nil {
		return nil, err
	}
	switch t := o.str("type"); t {
	case "Sequence":
		var items []json.RawMessage
		if err := json.Unmarshal(o["pretokenizers"], &items); err != nil {
			return nil, err
		}
		var seq seqPre
		for _, it := range items {
			p, err := parsePreTokenizer(it)
			if err != nil {
				return nil, err
			}
			if p != nil {
				seq = append(seq, p)
			}
		}
		return seq, nil
	case "BertPreTokenizer":
		return bertPre{}, nil
	case "Whitespace":
		return whitespacePre{}, nil
	case "WhitespaceSplit":
		return whitespaceSplitPre{}, nil
	case "Punctuation":
		b := o.str("behavior")
		if b == "" {
			b = "Isolated"
		}
		return punctuationPre{behaviorName(b)}, nil
	case "Digits":
		return digitsPre{o.boolean("individual_digits", false)}, nil
	case "ByteLevel":
		return byteLevelPre{addPrefixSpace: o.boolean("add_prefix_space", true), useRegex: o.boolean("use_regex", true)}, nil
	case "Metaspace":
		scheme := o.str("prepend_scheme")
		if scheme == "" {
			if o.boolean("add_prefix_space", true) {
				scheme = "always"
			} else {
				scheme = "never"
			}
		}
		repl := o.str("replacement")
		if repl == "" {
			repl = "▁"
		}
		return metaspacePre{repl: repl, scheme: scheme, split: o.boolean("split", true)}, nil
	case "Split":
		s, re, err := parsePattern(o["pattern"])
		if err != nil {
			return nil, err
		}
		return splitPre{str: s, re: re, behavior: behaviorName(o.str("behavior")), invert: o.boolean("invert", false)}, nil
	default:
		return nil, fmt.Errorf("tokenizer: unsupported pre tokenizer %q", t)
	}
}
