//go:build onnx

package onnx

import (
	"context"
	"errors"
	"fmt"

	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

type Embedder struct {
	tk     *tokenizer.Tokenizer
	p      *pool
	maxLen int
	batch  int
	name   string
}

func newEmbedder(src source, tk *tokenizer.Tokenizer, cfg Config) (*Embedder, error) {
	p, err := newPool(src, cfg.PoolSize, cfg.Threads)
	if err != nil {
		return nil, err
	}
	if !p.has("input_ids") {
		p.close()
		return nil, errors.New("onnx: embedder model has no input_ids input")
	}
	name := cfg.EmbedName
	if name == "" {
		name = "onnx.embed"
	}
	return &Embedder{tk: tk, p: p, maxLen: cfg.MaxLen, batch: cfg.BatchSize, name: name}, nil
}

func (e *Embedder) Name() string { return e.name }

func (e *Embedder) Close() { e.p.close() }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for start := 0; start < len(texts); start += e.batch {
		end := start + e.batch
		if end > len(texts) {
			end = len(texts)
		}
		ids := make([][]int, end-start)
		tts := make([][]int, end-start)
		for i := start; i < end; i++ {
			enc := e.tk.EncodeTruncated(texts[i], e.maxLen)
			ids[i-start], tts[i-start] = enc.IDs, enc.TypeIDs
			if len(ids[i-start]) == 0 {
				ids[i-start] = []int{e.tk.PadID()}
			}
		}
		res, err := e.p.run(ctx, seqFeeds(ids, tts, e.tk.PadID()))
		if err != nil {
			return nil, err
		}
		vecs, err := pooled(res, ids)
		if err != nil {
			return nil, err
		}
		copy(out[start:end], vecs)
	}
	return out, nil
}

func pooled(res *output, ids [][]int) ([][]float32, error) {
	b := len(ids)
	switch len(res.shape) {
	case 2:
		if int(res.shape[0]) != b {
			return nil, fmt.Errorf("onnx: unexpected embedding shape %v", res.shape)
		}
		h := int(res.shape[1])
		out := make([][]float32, b)
		for i := range out {
			out[i] = append([]float32(nil), res.data[i*h:(i+1)*h]...)
		}
		return out, nil
	case 3:
		if int(res.shape[0]) != b {
			return nil, fmt.Errorf("onnx: unexpected hidden state shape %v", res.shape)
		}
		t, h := int(res.shape[1]), int(res.shape[2])
		out := make([][]float32, b)
		for i := range out {
			v := make([]float32, h)
			n := len(ids[i])
			if n > t {
				n = t
			}
			for j := 0; j < n; j++ {
				row := res.data[(i*t+j)*h : (i*t+j+1)*h]
				for k := range v {
					v[k] += row[k]
				}
			}
			if n > 0 {
				for k := range v {
					v[k] /= float32(n)
				}
			}
			out[i] = v
		}
		return out, nil
	}
	return nil, fmt.Errorf("onnx: unexpected embedding rank %v", res.shape)
}
