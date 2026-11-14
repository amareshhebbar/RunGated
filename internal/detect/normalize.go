package detect

import (
	"encoding/base64"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

var confusables = map[rune]rune{
	'а': 'a', 'е': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'у': 'y', 'х': 'x', 'і': 'i', 'ј': 'j', 'ѕ': 's', 'ԁ': 'd', 'ɡ': 'g', 'һ': 'h', 'ӏ': 'l', 'ո': 'n', 'ս': 'u',
	'А': 'a', 'В': 'b', 'Е': 'e', 'К': 'k', 'М': 'm', 'Н': 'h', 'О': 'o', 'Р': 'p', 'С': 'c', 'Т': 't', 'Х': 'x', 'У': 'y', 'І': 'i',
	'α': 'a', 'ο': 'o', 'ν': 'v', 'ι': 'i', 'κ': 'k', 'ρ': 'p', 'τ': 't', 'υ': 'u', 'χ': 'x', 'Α': 'a', 'Β': 'b', 'Ε': 'e', 'Ζ': 'z', 'Η': 'h', 'Ι': 'i', 'Κ': 'k', 'Μ': 'm', 'Ν': 'n', 'Ο': 'o', 'Ρ': 'p', 'Τ': 't', 'Υ': 'y', 'Χ': 'x',
	'ⅰ': 'i', 'ℓ': 'l', '‘': '\'', '’': '\'', '“': '"', '”': '"', '‐': '-', '‑': '-', '–': '-', '—': '-',
}

var leet = map[rune]rune{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '@': 'a', '$': 's', '!': 'i', '|': 'l'}

func isInvisible(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064, r == 0xFEFF, r == 0x00AD, r == 0x180E, r >= 0xFE00 && r <= 0xFE0F:
		return true
	case r >= 0xE0000 && r <= 0xE007F:
		return true
	}
	return false
}

type Normalized struct {
	Text       string
	Leet       string
	Collapsed  string
	Squashed   string
	Invisible  int
	TagPayload string
	Decoded    []string
}

var b64Re = regexp.MustCompile(`[A-Za-z0-9+/]{24,}={0,2}`)
var hexRe = regexp.MustCompile(`\b(?:[0-9a-fA-F]{2}){16,}\b`)
var spacedRe = regexp.MustCompile(`\b(?:[a-z] ){4,}[a-z]\b`)
var rot13Hint = regexp.MustCompile(`(?i)\brot-?13\b`)

func rot13(s string) string {
	b := []byte(s)
	for i, c := range b {
		switch {
		case c >= 'a' && c <= 'z':
			b[i] = 'a' + (c-'a'+13)%26
		case c >= 'A' && c <= 'Z':
			b[i] = 'A' + (c-'A'+13)%26
		}
	}
	return string(b)
}

func collapseLetters(s string) string {
	return spacedRe.ReplaceAllStringFunc(s, func(m string) string { return strings.ReplaceAll(m, " ", "") })
}

func Normalize(s string) Normalized {
	var n Normalized
	var tags strings.Builder
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0xE0020 && r <= 0xE007E {
			tags.WriteRune(r - 0xE0000)
			n.Invisible++
			continue
		}
		if isInvisible(r) {
			n.Invisible++
			continue
		}
		b.WriteRune(r)
	}
	n.TagPayload = tags.String()
	t := b.String()
	if !isASCII(t) {
		t = norm.NFKC.String(t)
	}
	n.Text = collapseSpace(strings.ToLower(foldConfusables(t)))
	var l strings.Builder
	l.Grow(len(n.Text))
	for _, r := range n.Text {
		if m, ok := leet[r]; ok {
			r = m
		}
		l.WriteRune(r)
	}
	n.Leet = l.String()
	if col := collapseLetters(n.Text); col != n.Text {
		n.Collapsed = col
	}
	n.Squashed = squash(n.Leet)
	if len(s) < 200000 {
		for _, m := range hexRe.FindAllString(s, 8) {
			dec := make([]byte, len(m)/2)
			for i := 0; i < len(dec); i++ {
				var v byte
				for _, ch := range []byte{m[2*i], m[2*i+1]} {
					v <<= 4
					switch {
					case ch >= '0' && ch <= '9':
						v |= ch - '0'
					case ch >= 'a' && ch <= 'f':
						v |= ch - 'a' + 10
					default:
						v |= ch - 'A' + 10
					}
				}
				dec[i] = v
			}
			if utf8.Valid(dec) && printableRatio(dec) > 0.9 {
				n.Decoded = append(n.Decoded, strings.ToLower(string(dec)))
			}
		}
		if rot13Hint.MatchString(s) {
			n.Decoded = append(n.Decoded, strings.ToLower(rot13(s)))
		}
		for _, m := range b64Re.FindAllString(s, 8) {
			dec, err := base64.StdEncoding.DecodeString(padB64(m))
			if err != nil {
				dec, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(m, "="))
			}
			if err == nil && utf8.Valid(dec) && printableRatio(dec) > 0.9 {
				n.Decoded = append(n.Decoded, strings.ToLower(string(dec)))
			}
		}
	}
	return n
}

func padB64(s string) string {
	if m := len(s) % 4; m != 0 {
		return s + strings.Repeat("=", 4-m)
	}
	return s
}

func printableRatio(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	p := 0
	for _, r := range string(b) {
		if unicode.IsPrint(r) || unicode.IsSpace(r) {
			p++
		}
	}
	return float64(p) / float64(utf8.RuneCount(b))
}

func Entropy(s string) float64 {
	if s == "" {
		return 0
	}
	freq := map[rune]float64{}
	n := 0.0
	for _, r := range s {
		freq[r]++
		n++
	}
	e := 0.0
	for _, c := range freq {
		p := c / n
		e -= p * math.Log2(p)
	}
	return e
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	sp := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !sp {
				b.WriteByte(' ')
			}
			sp = true
			continue
		}
		sp = false
		b.WriteRune(r)
	}
	return b.String()
}

func isLatinLetter(r rune) bool {
	return r < 0x250 && unicode.IsLetter(r)
}

func foldConfusables(t string) string {
	var out strings.Builder
	out.Grow(len(t))
	word := make([]rune, 0, 32)
	flush := func() {
		if len(word) == 0 {
			return
		}
		latin, other := 0, 0
		for _, r := range word {
			if isLatinLetter(r) {
				latin++
			} else if _, ok := confusables[r]; ok {
				other++
			} else if unicode.IsLetter(r) {
				other++
			}
		}
		fold := latin > 0 && other > 0
		for _, r := range word {
			if m, ok := confusables[r]; ok && (fold || !unicode.IsLetter(r)) {
				r = m
			}
			out.WriteRune(r)
		}
		word = word[:0]
	}
	for _, r := range t {
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) {
			word = append(word, r)
			continue
		}
		flush()
		if m, ok := confusables[r]; ok {
			r = m
		}
		out.WriteRune(r)
	}
	flush()
	return out.String()
}

func squash(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
