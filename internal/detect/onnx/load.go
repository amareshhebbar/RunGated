//go:build onnx

package onnx

import (
	"fmt"
	"path/filepath"

	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

const Embedded = "embedded"

func tokenizerFor(explicit, modelPath string) (*tokenizer.Tokenizer, error) {
	if explicit == Embedded || (explicit == "" && modelPath == Embedded) {
		b, ok := embeddedFile("tokenizer.json")
		if !ok {
			return nil, fmt.Errorf("no embedded tokenizer: rebuild with -tags onnx,onnx_embed and place files in internal/detect/onnx/models")
		}
		return tokenizer.FromJSON(b)
	}
	path := explicit
	if path == "" {
		path = filepath.Join(filepath.Dir(modelPath), "tokenizer.json")
	}
	tk, err := tokenizer.Load(path)
	if err != nil {
		return nil, fmt.Errorf("tokenizer %s: %w", path, err)
	}
	return tk, nil
}

func Load(cfg Config) (*Models, error) {
	cfg = cfg.withDefaults()
	if cfg.ClassifierPath == "" && cfg.NERPath == "" && cfg.EmbedPath == "" {
		return nil, fmt.Errorf("onnx: no model path is set")
	}
	if err := initRuntime(cfg.LibraryPath); err != nil {
		return nil, fmt.Errorf("onnx runtime: %w", err)
	}
	m := &Models{}
	if cfg.ClassifierPath != "" {
		tk, err := tokenizerFor(cfg.TokenizerPath, cfg.ClassifierPath)
		if err != nil {
			return nil, err
		}
		src, err := resolveSource(cfg.ClassifierPath, "classifier.onnx")
		if err != nil {
			m.Close()
			return nil, err
		}
		c, err := newClassifier(src, tk, cfg)
		if err != nil {
			m.Close()
			return nil, fmt.Errorf("classifier %s: %w", cfg.ClassifierPath, err)
		}
		m.Classifier = c
		m.closers = append(m.closers, c.Close)
	}
	if cfg.NERPath != "" {
		ntk := cfg.NERTokenizerPath
		if ntk == "" {
			ntk = cfg.TokenizerPath
		}
		tk, err := tokenizerFor(ntk, cfg.NERPath)
		if err != nil {
			m.Close()
			return nil, err
		}
		var lf *labelFile
		if cfg.NERPath == Embedded {
			b, ok := embeddedFile("labels.json")
			if !ok {
				m.Close()
				return nil, fmt.Errorf("no embedded labels.json")
			}
			lf, err = parseLabels(b)
		} else {
			labelsPath := cfg.NERLabelsPath
			if labelsPath == "" {
				labelsPath = filepath.Join(filepath.Dir(cfg.NERPath), "labels.json")
			}
			lf, err = loadLabels(labelsPath)
		}
		if err != nil {
			m.Close()
			return nil, err
		}
		src, err := resolveSource(cfg.NERPath, "ner.onnx")
		if err != nil {
			m.Close()
			return nil, err
		}
		probe, err := newPool(src, 1, 1)
		if err != nil {
			m.Close()
			return nil, fmt.Errorf("ner %s: %w", cfg.NERPath, err)
		}
		gl := probe.has("span_idx")
		probe.close()
		if gl {
			g, err := newGLiNER(src, tk, lf, cfg)
			if err != nil {
				m.Close()
				return nil, err
			}
			m.NER = g
			m.closers = append(m.closers, g.Close)
		} else {
			n, err := newNER(src, tk, lf, cfg)
			if err != nil {
				m.Close()
				return nil, err
			}
			m.NER = n
			m.closers = append(m.closers, n.Close)
		}
	}
	if cfg.EmbedPath != "" {
		tk, err := tokenizerFor(cfg.EmbedTokenizerPath, cfg.EmbedPath)
		if err != nil {
			m.Close()
			return nil, err
		}
		e, err := newEmbedder(source{path: cfg.EmbedPath}, tk, cfg)
		if err != nil {
			m.Close()
			return nil, fmt.Errorf("embedder %s: %w", cfg.EmbedPath, err)
		}
		m.Embedder = e
		m.closers = append(m.closers, e.Close)
	}
	return m, nil
}

func resolveSource(path, embeddedName string) (source, error) {
	if path != Embedded {
		return source{path: path}, nil
	}
	b, ok := embeddedFile(embeddedName)
	if !ok {
		return source{}, fmt.Errorf("no embedded %s: rebuild with -tags onnx,onnx_embed", embeddedName)
	}
	return source{data: b}, nil
}
