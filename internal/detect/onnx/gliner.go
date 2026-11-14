//go:build onnx

package onnx

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

var wordRE = regexp.MustCompile(`[\p{L}\p{N}_]+(?:[-_][\p{L}\p{N}_]+)*|\S`)

type GLiNER struct {
	tk        *tokenizer.Tokenizer
	p         *pool
	labels    []string
	prompt    []int
	maxWidth  int
	maxLen    int
	threshold float64
	bos, eos  int
	hasBounds bool
}

func newGLiNER(src source, tk *tokenizer.Tokenizer, lf *labelFile, cfg Config) (*GLiNER, error) {
	entID, ok1 := tk.TokenToID("<<ENT>>")
	sepID, ok2 := tk.TokenToID("<<SEP>>")
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("onnx: gliner tokenizer lacks <<ENT>> or <<SEP>> tokens")
	}
	p, err := newPool(src, cfg.PoolSize, cfg.Threads)
	if err != nil {
		return nil, err
	}
	g := &GLiNER{tk: tk, p: p, labels: lf.Labels, maxWidth: lf.MaxWidth, maxLen: lf.MaxLen, threshold: lf.Threshold}
	if g.maxWidth <= 0 {
		g.maxWidth = 12
	}
	if g.maxLen <= 0 {
		g.maxLen = 384
	}
	if g.threshold <= 0 {
		g.threshold = cfg.NERThreshold
	}
	special := tk.Encode("", true).IDs
	if len(special) >= 2 {
		g.bos, g.eos, g.hasBounds = special[0], special[len(special)-1], true
	}
	for _, l := range lf.Labels {
		g.prompt = append(g.prompt, entID)
		g.prompt = append(g.prompt, g.wordIDs(l)...)
	}
	g.prompt = append(g.prompt, sepID)
	return g, nil
}

func (g *GLiNER) Name() string { return "onnx.gliner" }

func (g *GLiNER) Close() { g.p.close() }

func (g *GLiNER) wordIDs(w string) []int {
	var ids []int
	for _, part := range strings.Fields(w) {
		ids = append(ids, g.tk.Encode(part, false).IDs...)
	}
	return ids
}

type gword struct {
	start, end int
	ids        []int
}

func (g *GLiNER) Extract(ctx context.Context, text string) ([]detect.Entity, error) {
	locs := wordRE.FindAllStringIndex(text, -1)
	words := make([]gword, len(locs))
	for i, l := range locs {
		words[i] = gword{start: l[0], end: l[1], ids: g.tk.Encode(text[l[0]:l[1]], false).IDs}
	}
	budget := g.maxLen - len(g.prompt) - 2
	if budget < 8 {
		return nil, fmt.Errorf("onnx: gliner max_len %d too small for %d labels", g.maxLen, len(g.labels))
	}
	var all []detect.Entity
	i := 0
	for i < len(words) {
		used, j := 0, i
		for j < len(words) && used+len(words[j].ids)+1 <= budget {
			used += len(words[j].ids)
			j++
		}
		if j == i {
			j = i + 1
		}
		ents, err := g.chunk(ctx, text, words[i:j])
		if err != nil {
			return nil, err
		}
		all = append(all, ents...)
		i = j
	}
	return dedupe(all), nil
}

func (g *GLiNER) chunk(ctx context.Context, text string, words []gword) ([]detect.Entity, error) {
	var ids, wmask []int64
	if g.hasBounds {
		ids = append(ids, int64(g.bos))
		wmask = append(wmask, 0)
	}
	for _, id := range g.prompt {
		ids = append(ids, int64(id))
		wmask = append(wmask, 0)
	}
	for wi, w := range words {
		if len(w.ids) == 0 {
			ids = append(ids, int64(g.tk.PadID()))
			wmask = append(wmask, int64(wi+1))
			continue
		}
		for k, id := range w.ids {
			ids = append(ids, int64(id))
			if k == 0 {
				wmask = append(wmask, int64(wi+1))
			} else {
				wmask = append(wmask, 0)
			}
		}
	}
	if g.hasBounds {
		ids = append(ids, int64(g.eos))
		wmask = append(wmask, 0)
	}
	l := int64(len(ids))
	attn := make([]int64, l)
	for i := range attn {
		attn[i] = 1
	}
	nw := len(words)
	spans := make([]int64, 0, nw*g.maxWidth*2)
	smask := make([]bool, 0, nw*g.maxWidth)
	for s := 0; s < nw; s++ {
		for w := 0; w < g.maxWidth; w++ {
			spans = append(spans, int64(s), int64(s+w))
			smask = append(smask, s+w < nw)
		}
	}
	feeds := map[string]feed{
		"input_ids":      {shape: []int64{1, l}, i64: ids},
		"attention_mask": {shape: []int64{1, l}, i64: attn},
		"words_mask":     {shape: []int64{1, l}, i64: wmask},
		"text_lengths":   {shape: []int64{1, 1}, i64: []int64{int64(nw)}},
		"span_idx":       {shape: []int64{1, int64(nw * g.maxWidth), 2}, i64: spans},
		"span_mask":      {shape: []int64{1, int64(nw * g.maxWidth)}, b: smask},
	}
	res, err := g.p.run(ctx, feeds)
	if err != nil {
		return nil, err
	}
	c := len(g.labels)
	if len(res.shape) != 4 || int(res.shape[2]) != g.maxWidth || int(res.shape[3]) != c || int(res.shape[1]) < nw {
		return nil, fmt.Errorf("onnx: unexpected gliner output shape %v", res.shape)
	}
	type cand struct {
		s, e, label int
		score       float64
	}
	var cands []cand
	stride := int(res.shape[1])
	for s := 0; s < nw; s++ {
		for w := 0; w < g.maxWidth && s+w < nw; w++ {
			base := (s*g.maxWidth + w) * c
			if s >= stride {
				continue
			}
			for k := 0; k < c; k++ {
				if p := sigmoid(res.data[base+k]); p >= g.threshold {
					cands = append(cands, cand{s, s + w, k, p})
				}
			}
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	taken := make([]bool, nw)
	var out []detect.Entity
	for _, cd := range cands {
		free := true
		for x := cd.s; x <= cd.e; x++ {
			if taken[x] {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for x := cd.s; x <= cd.e; x++ {
			taken[x] = true
		}
		a, b := words[cd.s].start, words[cd.e].end
		out = append(out, detect.Entity{Label: strings.ToLower(g.labels[cd.label]), Start: a, End: b, Score: cd.score, Text: text[a:b]})
	}
	return out, nil
}
