package tokenizer

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

type tok struct {
	id    int
	value string
	s, e  int
}

type model interface {
	Tokenize(piece NString) []tok
	TokenToID(string) (int, bool)
	IDToToken(int) (string, bool)
}

type wordPiece struct {
	vocab     map[string]int
	inv       map[int]string
	unk       string
	prefix    string
	maxChars  int
	unkID     int
	hasUnkVoc bool
}

func (w *wordPiece) TokenToID(s string) (int, bool)  { id, ok := w.vocab[s]; return id, ok }
func (w *wordPiece) IDToToken(id int) (string, bool) { s, ok := w.inv[id]; return s, ok }

func (w *wordPiece) Tokenize(p NString) []tok {
	n := p.Len()
	if n > w.maxChars {
		return []tok{{id: w.unkID, value: w.unk, s: 0, e: n}}
	}
	var out []tok
	start := 0
	for start < n {
		end := n
		found := -1
		var val string
		for start < end {
			sub := string(p.R[start:end])
			if start > 0 {
				sub = w.prefix + sub
			}
			if id, ok := w.vocab[sub]; ok {
				found = id
				val = sub
				break
			}
			end--
		}
		if found < 0 {
			return []tok{{id: w.unkID, value: w.unk, s: 0, e: n}}
		}
		out = append(out, tok{id: found, value: val, s: start, e: end})
		start = end
	}
	return out
}

type bpe struct {
	vocab       map[string]int
	inv         map[int]string
	ranks       map[[2]string]int
	unk         string
	prefix      string
	suffix      string
	fuseUnk     bool
	byteFB      bool
	ignoreMerge bool
}

func (b *bpe) TokenToID(s string) (int, bool)  { id, ok := b.vocab[s]; return id, ok }
func (b *bpe) IDToToken(id int) (string, bool) { s, ok := b.inv[id]; return s, ok }

type sym struct {
	val  string
	s, e int
	unk  bool
}

func (b *bpe) Tokenize(p NString) []tok {
	n := p.Len()
	if n == 0 {
		return nil
	}
	if b.ignoreMerge {
		if id, ok := b.vocab[p.String()]; ok {
			return []tok{{id: id, value: p.String(), s: 0, e: n}}
		}
	}
	var syms []sym
	for i := 0; i < n; i++ {
		c := string(p.R[i])
		if i > 0 && b.prefix != "" {
			c = b.prefix + c
		}
		if i == n-1 && b.suffix != "" {
			c += b.suffix
		}
		if _, ok := b.vocab[c]; ok {
			syms = append(syms, sym{val: c, s: i, e: i + 1})
			continue
		}
		if b.byteFB {
			var bs []sym
			okAll := true
			for _, by := range []byte(string(p.R[i])) {
				bt := fmt.Sprintf("<0x%02X>", by)
				if _, ok := b.vocab[bt]; !ok {
					okAll = false
					break
				}
				bs = append(bs, sym{val: bt, s: i, e: i + 1})
			}
			if okAll {
				syms = append(syms, bs...)
				continue
			}
		}
		if b.unk != "" {
			if b.fuseUnk && len(syms) > 0 && syms[len(syms)-1].unk {
				syms[len(syms)-1].e = i + 1
			} else {
				syms = append(syms, sym{val: b.unk, s: i, e: i + 1, unk: true})
			}
		}
	}
	for {
		best, bestRank := -1, math.MaxInt
		for i := 0; i+1 < len(syms); i++ {
			if r, ok := b.ranks[[2]string{syms[i].val, syms[i+1].val}]; ok && r < bestRank {
				merged := b.mergedValue(syms[i].val, syms[i+1].val)
				if _, ok := b.vocab[merged]; ok {
					best, bestRank = i, r
				}
			}
		}
		if best < 0 {
			break
		}
		merged := b.mergedValue(syms[best].val, syms[best+1].val)
		syms[best] = sym{val: merged, s: syms[best].s, e: syms[best+1].e}
		syms = append(syms[:best+1], syms[best+2:]...)
	}
	out := make([]tok, 0, len(syms))
	for _, s := range syms {
		id := b.vocab[s.val]
		out = append(out, tok{id: id, value: s.val, s: s.s, e: s.e})
	}
	return out
}

func (b *bpe) mergedValue(a, c string) string {
	if b.prefix != "" && strings.HasPrefix(c, b.prefix) {
		c = c[len(b.prefix):]
	}
	return a + c
}

type unigram struct {
	pieces   []string
	scores   []float64
	vocab    map[string]int
	unkID    int
	hasUnk   bool
	byteFB   bool
	minScore float64
	maxRunes int
}

func (u *unigram) TokenToID(s string) (int, bool) { id, ok := u.vocab[s]; return id, ok }
func (u *unigram) IDToToken(id int) (string, bool) {
	if id < 0 || id >= len(u.pieces) {
		return "", false
	}
	return u.pieces[id], true
}

func (u *unigram) Tokenize(p NString) []tok {
	n := p.Len()
	if n == 0 {
		return nil
	}
	type node struct {
		id    int
		score float64
		start int
		set   bool
	}
	best := make([]node, n+1)
	unkScore := u.minScore - 10.0
	for start := 0; start < n; start++ {
		base := best[start].score
		hasSingle := false
		maxEnd := start + u.maxRunes
		if maxEnd > n {
			maxEnd = n
		}
		for end := start + 1; end <= maxEnd; end++ {
			id, ok := u.vocab[string(p.R[start:end])]
			if !ok {
				continue
			}
			cand := u.scores[id] + base
			t := &best[end]
			if !t.set || cand > t.score {
				t.score, t.start, t.id, t.set = cand, start, id, true
			}
			if end == start+1 {
				hasSingle = true
			}
		}
		if !hasSingle {
			cand := unkScore + base
			t := &best[start+1]
			if !t.set || cand > t.score {
				t.score, t.start, t.id, t.set = cand, start, u.unkID, true
			}
		}
	}
	type rng struct{ a, b int }
	var rev []rng
	var fused []rng
	flush := func() {
		if len(fused) > 0 {
			rev = append(rev, rng{fused[len(fused)-1].a, fused[0].b})
			fused = nil
		}
	}
	for end := n; end > 0; {
		nd := best[end]
		if u.hasUnk && nd.id == u.unkID {
			fused = append(fused, rng{nd.start, end})
		} else {
			flush()
			rev = append(rev, rng{nd.start, end})
		}
		end = nd.start
	}
	flush()
	var out []tok
	for i := len(rev) - 1; i >= 0; i-- {
		r := rev[i]
		str := string(p.R[r.a:r.b])
		if id, ok := u.vocab[str]; ok {
			out = append(out, tok{id: id, value: str, s: r.a, e: r.b})
			continue
		}
		if u.byteFB {
			var bs []tok
			okAll := true
			for _, by := range []byte(str) {
				bt := fmt.Sprintf("<0x%02X>", by)
				id, ok := u.vocab[bt]
				if !ok {
					okAll = false
					break
				}
				bs = append(bs, tok{id: id, value: bt, s: r.a, e: r.b})
			}
			if okAll {
				out = append(out, bs...)
				continue
			}
		}
		out = append(out, tok{id: u.unkID, value: str, s: r.a, e: r.b})
	}
	return out
}

func parseModel(raw json.RawMessage) (model, error) {
	o, err := parseObj(raw)
	if err != nil || o == nil {
		return nil, fmt.Errorf("tokenizer: missing model")
	}
	typ := o.str("type")
	if typ == "" {
		if _, ok := o["merges"]; ok {
			typ = "BPE"
		} else {
			typ = "WordPiece"
		}
	}
	switch typ {
	case "WordPiece":
		var vocab map[string]int
		if err := json.Unmarshal(o["vocab"], &vocab); err != nil {
			return nil, err
		}
		w := &wordPiece{vocab: vocab, inv: invert(vocab), unk: o.str("unk_token"), prefix: o.str("continuing_subword_prefix"), maxChars: o.num("max_input_chars_per_word", 100)}
		if id, ok := vocab[w.unk]; ok {
			w.unkID = id
		}
		return w, nil
	case "BPE":
		var vocab map[string]int
		if err := json.Unmarshal(o["vocab"], &vocab); err != nil {
			return nil, err
		}
		var rawMerges []json.RawMessage
		if err := json.Unmarshal(o["merges"], &rawMerges); err != nil {
			return nil, err
		}
		b := &bpe{vocab: vocab, inv: invert(vocab), ranks: make(map[[2]string]int, len(rawMerges)), prefix: o.str("continuing_subword_prefix"), suffix: o.str("end_of_word_suffix"), fuseUnk: o.boolean("fuse_unk", false), byteFB: o.boolean("byte_fallback", false), ignoreMerge: o.boolean("ignore_merges", false)}
		if v, ok := o["unk_token"]; ok && string(v) != "null" {
			json.Unmarshal(v, &b.unk)
		}
		for i, m := range rawMerges {
			var pair [2]string
			var s string
			if json.Unmarshal(m, &s) == nil {
				parts := strings.SplitN(s, " ", 2)
				if len(parts) != 2 {
					return nil, fmt.Errorf("tokenizer: bad merge %q", s)
				}
				pair = [2]string{parts[0], parts[1]}
			} else if err := json.Unmarshal(m, &pair); err != nil {
				return nil, err
			}
			if _, dup := b.ranks[pair]; !dup {
				b.ranks[pair] = i
			}
		}
		return b, nil
	case "Unigram":
		var vocab [][2]json.RawMessage
		if err := json.Unmarshal(o["vocab"], &vocab); err != nil {
			return nil, err
		}
		u := &unigram{vocab: make(map[string]int, len(vocab)), byteFB: o.boolean("byte_fallback", false), minScore: math.Inf(1)}
		for i, v := range vocab {
			var piece string
			var score float64
			json.Unmarshal(v[0], &piece)
			json.Unmarshal(v[1], &score)
			u.pieces = append(u.pieces, piece)
			u.scores = append(u.scores, score)
			u.vocab[piece] = i
			if score < u.minScore {
				u.minScore = score
			}
			if l := utf8.RuneCountInString(piece); l > u.maxRunes {
				u.maxRunes = l
			}
		}
		if v, ok := o["unk_id"]; ok && string(v) != "null" {
			json.Unmarshal(v, &u.unkID)
			u.hasUnk = true
		}
		return u, nil
	default:
		return nil, fmt.Errorf("tokenizer: unsupported model %q", typ)
	}
}

func invert(m map[string]int) map[int]string {
	out := make(map[int]string, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}
