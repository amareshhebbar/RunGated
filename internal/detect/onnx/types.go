package onnx

import (
	"errors"

	"github.com/amareshhebbar/rungated/internal/detect"
)

var ErrUnavailable = errors.New("onnx support not compiled in: rebuild with -tags onnx and CGO_ENABLED=1")

type Config struct {
	ClassifierPath     string
	NERPath            string
	NERLabelsPath      string
	TokenizerPath      string
	NERTokenizerPath   string
	EmbedPath          string
	EmbedTokenizerPath string
	EmbedName          string
	LibraryPath        string
	PoolSize           int
	Threads            int
	MaxLen             int
	Stride             int
	MaxWindows         int
	BatchSize          int
	InjectionIndex     *int
	NERThreshold       float64
	Temperature        float64
}

func (c Config) withDefaults() Config {
	if c.PoolSize <= 0 {
		c.PoolSize = 2
	}
	if c.Threads <= 0 {
		c.Threads = 1
	}
	if c.MaxLen <= 0 {
		c.MaxLen = 512
	}
	if c.Stride <= 0 {
		c.Stride = c.MaxLen / 4
	}
	if c.MaxWindows <= 0 {
		c.MaxWindows = 8
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 8
	}
	if c.NERThreshold <= 0 {
		c.NERThreshold = 0.5
	}
	return c
}

type Models struct {
	Classifier detect.Classifier
	NER        detect.NER
	Embedder   detect.Embedder
	closers    []func()
}

func (m *Models) Close() {
	if m == nil {
		return
	}
	for _, c := range m.closers {
		c()
	}
	m.closers = nil
}
