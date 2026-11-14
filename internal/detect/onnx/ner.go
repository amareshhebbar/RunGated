//go:build onnx

package onnx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

type labelFile struct {
	Labels    []string          `json:"labels"`
	ID2Label  map[string]string `json:"id2label"`
	MaxWidth  int               `json:"max_width"`
	Threshold float64           `json:"threshold"`
	MaxLen    int               `json:"max_len"`
}

func loadLabels(path string) (*labelFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLabels(b)
}

func parseLabels(b []byte) (*labelFile, error) {
	var lf labelFile
	if err := json.Unmarshal(b, &lf); err != nil {
		return nil, err
	}
	if len(lf.Labels) == 0 && len(lf.ID2Label) > 0 {
		lf.Labels = make([]string, len(lf.ID2Label))
		for k, v := range lf.ID2Label {
			i, err := strconv.Atoi(k)
			if err != nil || i < 0 || i >= len(lf.Labels) {
				return nil, fmt.Errorf("onnx: bad id2label key %q", k)
			}
			lf.Labels[i] = v
		}
	}
	if len(lf.Labels) == 0 {
		return nil, fmt.Errorf("onnx: no labels found")
	}
	return &lf, nil
}

type NER struct {
	tk     *tokenizer.Tokenizer
	p      *pool
	labels []string
	maxLen int
	stride int
	maxWin int
	minP   float64
}

func newNER(src source, tk *tokenizer.Tokenizer, lf *labelFile, cfg Config) (*NER, error) {
	p, err := newPool(src, cfg.PoolSize, cfg.Threads)
	if err != nil {
		return nil, err
	}
	return &NER{tk: tk, p: p, labels: lf.Labels, maxLen: cfg.MaxLen, stride: cfg.Stride, maxWin: cfg.MaxWindows, minP: cfg.NERThreshold}, nil
}

func (n *NER) Name() string { return "onnx.ner" }

func (n *NER) Close() { n.p.close() }

func splitBIO(label string) (string, string) {
	if len(label) > 2 && (label[1] == '-' || label[1] == '_') {
		switch label[0] {
		case 'B', 'I', 'E', 'S', 'L', 'U':
			return string(label[0]), label[2:]
		}
	}
	if label == "O" || label == "o" {
		return "O", ""
	}
	return "I", label
}

func (n *NER) Extract(ctx context.Context, text string) ([]detect.Entity, error) {
	var all []detect.Entity
	for _, w := range makeWindows(n.tk, text, n.maxLen, n.stride, n.maxWin) {
		enc := n.tk.EncodeTruncated(w.text, n.maxLen)
		if enc.Len() == 0 {
			continue
		}
		res, err := n.p.run(ctx, seqFeeds([][]int{enc.IDs}, [][]int{enc.TypeIDs}, n.tk.PadID()))
		if err != nil {
			return nil, err
		}
		if len(res.shape) != 3 || int(res.shape[1]) != enc.Len() || int(res.shape[2]) != len(n.labels) {
			return nil, fmt.Errorf("onnx: unexpected ner output shape %v for %d tokens and %d labels", res.shape, enc.Len(), len(n.labels))
		}
		all = append(all, n.decode(text, w, enc, res)...)
	}
	return dedupe(all), nil
}

func (n *NER) decode(text string, w window, enc *tokenizer.Encoding, res *output) []detect.Entity {
	k := len(n.labels)
	type cur struct {
		label      string
		start, end int
		sum        float64
		n          int
	}
	var out []detect.Entity
	var c *cur
	flush := func() {
		if c == nil {
			return
		}
		a, b := c.start, c.end
		for a < b {
			r, sz := utf8.DecodeRuneInString(text[a:b])
			if !unicode.IsSpace(r) {
				break
			}
			a += sz
		}
		for b > a {
			r, sz := utf8.DecodeLastRuneInString(text[a:b])
			if !unicode.IsSpace(r) {
				break
			}
			b -= sz
		}
		score := c.sum / float64(c.n)
		if b > a && score >= n.minP {
			out = append(out, detect.Entity{Label: strings.ToLower(c.label), Start: a, End: b, Score: score, Text: text[a:b]})
		}
		c = nil
	}
	for t := 0; t < enc.Len(); t++ {
		if enc.Special[t] == 1 || enc.Offsets[t][1] <= enc.Offsets[t][0] {
			continue
		}
		pr := softmax(res.data[t*k : (t+1)*k])
		best := 0
		for i, v := range pr {
			if v > pr[best] {
				best = i
			}
		}
		prefix, ent := splitBIO(n.labels[best])
		a, b := enc.Offsets[t][0]+w.byteOff, enc.Offsets[t][1]+w.byteOff
		if b > len(text) {
			b = len(text)
		}
		if prefix == "O" {
			flush()
			continue
		}
		continues := c != nil && c.label == ent && (prefix == "I" || prefix == "E" || prefix == "L")
		if !continues {
			flush()
			c = &cur{label: ent, start: a}
		}
		c.end = b
		c.sum += pr[best]
		c.n++
		if prefix == "S" || prefix == "U" || prefix == "E" || prefix == "L" {
			flush()
		}
	}
	flush()
	return out
}

func dedupe(es []detect.Entity) []detect.Entity {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Score != es[j].Score {
			return es[i].Score > es[j].Score
		}
		return es[i].Start < es[j].Start
	})
	var out []detect.Entity
	for _, e := range es {
		overlap := false
		for _, o := range out {
			if e.Start < o.End && o.Start < e.End {
				overlap = true
				break
			}
		}
		if !overlap {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}
