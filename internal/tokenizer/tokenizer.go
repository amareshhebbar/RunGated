package tokenizer

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"unicode"
)

type addedToken struct {
	ID         int    `json:"id"`
	Content    string `json:"content"`
	Special    bool   `json:"special"`
	Normalized bool   `json:"normalized"`
	LStrip     bool   `json:"lstrip"`
	RStrip     bool   `json:"rstrip"`
	SingleWord bool   `json:"single_word"`
	runes      []rune
}

type Tokenizer struct {
	norm   normalizer
	pre    preTokenizer
	model  model
	post   postProcessor
	added  []*addedToken
	addedM map[string]*addedToken
	padID  int
	padTok string
	maxLen int
}

type tokenizerFile struct {
	AddedTokens   []*addedToken   `json:"added_tokens"`
	Normalizer    json.RawMessage `json:"normalizer"`
	PreTokenizer  json.RawMessage `json:"pre_tokenizer"`
	PostProcessor json.RawMessage `json:"post_processor"`
	Model         json.RawMessage `json:"model"`
	Truncation    *struct {
		MaxLength int `json:"max_length"`
	} `json:"truncation"`
	Padding *struct {
		PadID    int    `json:"pad_id"`
		PadToken string `json:"pad_token"`
	} `json:"padding"`
}

func Load(path string) (*Tokenizer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return FromJSON(b)
}

func FromJSON(b []byte) (*Tokenizer, error) {
	var f tokenizerFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	t := &Tokenizer{addedM: map[string]*addedToken{}, padID: -1}
	var err error
	if t.norm, err = parseNormalizer(f.Normalizer); err != nil {
		return nil, err
	}
	if t.pre, err = parsePreTokenizer(f.PreTokenizer); err != nil {
		return nil, err
	}
	if t.model, err = parseModel(f.Model); err != nil {
		return nil, err
	}
	if t.post, err = parsePostProcessor(f.PostProcessor); err != nil {
		return nil, err
	}
	for _, a := range f.AddedTokens {
		a.runes = []rune(a.Content)
		t.added = append(t.added, a)
		t.addedM[a.Content] = a
	}
	sort.SliceStable(t.added, func(i, j int) bool { return len(t.added[i].runes) > len(t.added[j].runes) })
	if f.Padding != nil {
		t.padID, t.padTok = f.Padding.PadID, f.Padding.PadToken
	}
	if t.padID < 0 {
		for _, name := range []string{"[PAD]", "<pad>", "<|pad|>"} {
			if id, ok := t.TokenToID(name); ok {
				t.padID, t.padTok = id, name
				break
			}
		}
	}
	if t.padID < 0 {
		t.padID = 0
	}
	if f.Truncation != nil {
		t.maxLen = f.Truncation.MaxLength
	}
	for _, a := range t.added {
		if a.Normalized && t.norm != nil {
			a.runes = t.norm.Normalize(NewNString(a.Content)).R
		}
	}
	return t, nil
}

func (t *Tokenizer) TokenToID(s string) (int, bool) {
	if a, ok := t.addedM[s]; ok {
		return a.ID, true
	}
	return t.model.TokenToID(s)
}

func (t *Tokenizer) IDToToken(id int) (string, bool) {
	for _, a := range t.added {
		if a.ID == id {
			return a.Content, true
		}
	}
	return t.model.IDToToken(id)
}

func (t *Tokenizer) PadID() int { return t.padID }

func (t *Tokenizer) ModelMaxLen() int { return t.maxLen }

func (t *Tokenizer) NumSpecial(pair bool) int {
	if t.post == nil {
		return 0
	}
	return t.post.NumSpecial(pair)
}

type segment struct {
	text  NString
	added *addedToken
	s, e  int
}

func isWordChar(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func (t *Tokenizer) matchAt(rs []rune, i int, wantNormalized bool, lo int) (*addedToken, int, int) {
	for _, a := range t.added {
		if a.Normalized != wantNormalized || len(a.runes) == 0 || i+len(a.runes) > len(rs) {
			continue
		}
		ok := true
		for k, c := range a.runes {
			if rs[i+k] != c {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		start, end := i, i+len(a.runes)
		if a.SingleWord {
			if (start > 0 && isWordChar(rs[start-1])) || (end < len(rs) && isWordChar(rs[end])) {
				continue
			}
		}
		if a.LStrip {
			for start > lo && unicode.IsSpace(rs[start-1]) {
				start--
			}
		}
		if a.RStrip {
			for end < len(rs) && unicode.IsSpace(rs[end]) {
				end++
			}
		}
		return a, start, end
	}
	return nil, 0, 0
}

func (t *Tokenizer) splitAdded(n NString, wantNormalized bool) []segment {
	has := false
	for _, a := range t.added {
		if a.Normalized == wantNormalized {
			has = true
			break
		}
	}
	if !has || n.Len() == 0 {
		return []segment{{text: n}}
	}
	var out []segment
	last := 0
	i := 0
	for i < n.Len() {
		a, s, e := t.matchAt(n.R, i, wantNormalized, last)
		if a == nil {
			i++
			continue
		}
		if s > last {
			out = append(out, segment{text: n.Slice(last, s)})
		}
		out = append(out, segment{added: a, s: n.S[i], e: n.E[i+len(a.runes)-1]})
		last = e
		i = e
	}
	if last < n.Len() {
		out = append(out, segment{text: n.Slice(last, n.Len())})
	}
	return out
}

func (t *Tokenizer) encodeSequence(text string) *Encoding {
	enc := &Encoding{}
	raw := NewNString(text)
	for _, seg := range t.splitAdded(raw, false) {
		if seg.added != nil {
			enc.add(seg.added.ID, seg.added.Content, [2]int{seg.s, seg.e}, 0, 0)
			continue
		}
		n := seg.text
		if t.norm != nil {
			n = t.norm.Normalize(n)
		}
		for _, sub := range t.splitAdded(n, true) {
			if sub.added != nil {
				enc.add(sub.added.ID, sub.added.Content, [2]int{sub.s, sub.e}, 0, 0)
				continue
			}
			pieces := []NString{sub.text}
			if t.pre != nil {
				pieces = t.pre.Pre(pieces)
			}
			for _, p := range pieces {
				if p.Len() == 0 {
					continue
				}
				for _, tk := range t.model.Tokenize(p) {
					enc.add(tk.id, tk.value, [2]int{p.S[tk.s], p.E[tk.e-1]}, 0, 0)
				}
			}
		}
	}
	return enc
}

func truncate(e *Encoding, n int) {
	if n < 0 {
		n = 0
	}
	if e.Len() <= n {
		return
	}
	e.IDs, e.TypeIDs, e.Attn, e.Special, e.Tokens, e.Offsets = e.IDs[:n], e.TypeIDs[:n], e.Attn[:n], e.Special[:n], e.Tokens[:n], e.Offsets[:n]
}

func (t *Tokenizer) finish(a, b *Encoding, addSpecial bool) *Encoding {
	if t.post == nil {
		return defaultMerge(a, b)
	}
	return t.post.Process(a, b, addSpecial)
}

func (t *Tokenizer) Encode(text string, addSpecial bool) *Encoding {
	return t.finish(t.encodeSequence(text), nil, addSpecial)
}

func (t *Tokenizer) EncodePair(a, b string, addSpecial bool) *Encoding {
	return t.finish(t.encodeSequence(a), t.encodeSequence(b), addSpecial)
}

func (t *Tokenizer) EncodeTruncated(text string, maxLen int) *Encoding {
	a := t.encodeSequence(text)
	if maxLen > 0 {
		truncate(a, maxLen-t.NumSpecial(false))
	}
	return t.finish(a, nil, true)
}

func (t *Tokenizer) EncodePairTruncated(x, y string, maxLen int) *Encoding {
	a, b := t.encodeSequence(x), t.encodeSequence(y)
	if maxLen > 0 {
		budget := maxLen - t.NumSpecial(true)
		for a.Len()+b.Len() > budget && (a.Len() > 0 || b.Len() > 0) {
			if a.Len() >= b.Len() {
				truncate(a, a.Len()-1)
			} else {
				truncate(b, b.Len()-1)
			}
		}
	}
	return t.finish(a, b, true)
}

func (t *Tokenizer) Decode(ids []int, skipSpecial bool) string {
	var sb strings.Builder
	for _, id := range ids {
		s, ok := t.IDToToken(id)
		if !ok {
			continue
		}
		if a, ok := t.addedM[s]; ok && a.Special && skipSpecial {
			continue
		}
		sb.WriteString(s)
	}
	return sb.String()
}
