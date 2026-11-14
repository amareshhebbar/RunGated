//go:build onnx

package onnx

import (
	"context"
	"errors"
	"fmt"

	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

type Classifier struct {
	tk        *tokenizer.Tokenizer
	p         *pool
	maxLen    int
	stride    int
	maxWin    int
	batch     int
	posIndex  *int
	paired    bool
	tokenType bool
	temp      float64
}

func newClassifier(src source, tk *tokenizer.Tokenizer, cfg Config) (*Classifier, error) {
	p, err := newPool(src, cfg.PoolSize, cfg.Threads)
	if err != nil {
		return nil, err
	}
	if !p.has("input_ids") {
		p.close()
		return nil, errors.New("onnx: classifier model has no input_ids input")
	}
	return &Classifier{tk: tk, p: p, maxLen: cfg.MaxLen, stride: cfg.Stride, maxWin: cfg.MaxWindows, batch: cfg.BatchSize, posIndex: cfg.InjectionIndex, tokenType: p.has("token_type_ids"), temp: cfg.Temperature}, nil
}

func (c *Classifier) Name() string { return "onnx.injection" }

func (c *Classifier) Close() { c.p.close() }

type item struct {
	text int
	ids  []int
	tt   []int
}

func (c *Classifier) Classify(ctx context.Context, texts []string) ([]float64, error) {
	out := make([]float64, len(texts))
	var items []item
	for i, t := range texts {
		for _, w := range makeWindows(c.tk, t, c.maxLen, c.stride, c.maxWin) {
			enc := c.tk.EncodeTruncated(w.text, c.maxLen)
			if enc.Len() == 0 {
				continue
			}
			items = append(items, item{text: i, ids: enc.IDs, tt: enc.TypeIDs})
		}
	}
	for start := 0; start < len(items); start += c.batch {
		end := start + c.batch
		if end > len(items) {
			end = len(items)
		}
		chunk := items[start:end]
		ids := make([][]int, len(chunk))
		tts := make([][]int, len(chunk))
		for i, it := range chunk {
			ids[i], tts[i] = it.ids, it.tt
		}
		res, err := c.p.run(ctx, seqFeeds(ids, tts, c.tk.PadID()))
		if err != nil {
			return nil, err
		}
		if len(res.shape) != 2 || int(res.shape[0]) != len(chunk) {
			return nil, fmt.Errorf("onnx: unexpected classifier output shape %v", res.shape)
		}
		k := int(res.shape[1])
		for i, it := range chunk {
			row := res.data[i*k : (i+1)*k]
			if c.temp > 0 && c.temp != 1 {
				scaled := make([]float32, k)
				for x := range row {
					scaled[x] = row[x] / float32(c.temp)
				}
				row = scaled
			}
			var p float64
			if k == 1 {
				p = sigmoid(row[0])
			} else {
				idx := k - 1
				if c.posIndex != nil && *c.posIndex >= 0 && *c.posIndex < k {
					idx = *c.posIndex
				}
				p = softmax(row)[idx]
			}
			if p > out[it.text] {
				out[it.text] = p
			}
		}
	}
	return out, nil
}
