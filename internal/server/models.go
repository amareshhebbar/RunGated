package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/detect/cloud"
	"github.com/amareshhebbar/rungated/internal/detect/onnx"
	"github.com/amareshhebbar/rungated/internal/models"
)

func checkCloudModel(id string, kind models.Kind) error {
	if id == "" {
		return nil
	}
	m, ok := models.Find(id)
	if !ok {
		return fmt.Errorf("cloud: unknown model %q, see: rungated models list", id)
	}
	if m.Kind != kind {
		return fmt.Errorf("cloud: %s is a %s model, not %s", id, m.Kind, kind)
	}
	if !m.Hosted {
		return fmt.Errorf("cloud: %s is not offered hosted", id)
	}
	return nil
}

func embedPaths(cfg *config.Config) (string, string, error) {
	model, tok := cfg.Embed.ModelPath, cfg.Embed.TokenizerPath
	if model == "" && cfg.Embed.Model != "" {
		m, ok := models.Find(cfg.Embed.Model)
		if !ok || m.Kind != models.KindEmbed {
			return "", "", fmt.Errorf("embed: %q is not an embedder, see: rungated models list", cfg.Embed.Model)
		}
		if !m.Local {
			return "", "", fmt.Errorf("embed: %s is hosted only, use cloud.embedder: %s", m.ID, m.ID)
		}
		f, err := models.ModelFile("", m.ID)
		if err != nil {
			return "", "", err
		}
		model = f
	}
	if model == "" {
		return "", "", errors.New("embed: set embed.model or embed.model_path")
	}
	if tok == "" {
		tok = filepath.Join(filepath.Dir(model), "tokenizer.json")
	}
	return model, tok, nil
}

func (a *App) similarity(e detect.Embedder) (*detect.Similarity, error) {
	bank, err := detect.LoadBank(a.Cfg.Embed.BankPath)
	if err != nil {
		return nil, fmt.Errorf("embed bank: %w", err)
	}
	s := &detect.Similarity{Emb: e, Bank: bank, Threshold: a.Cfg.Embed.Threshold}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.Prepare(ctx); err != nil {
			slog.Warn("similarity bank not ready yet, will retry on first use", "embedder", e.Name(), "err", err)
		}
	}()
	return s, nil
}

func combine(list []detect.Classifier) detect.Classifier {
	switch len(list) {
	case 0:
		return nil
	case 1:
		return list[0]
	}
	return &detect.MaxClassifier{List: list}
}

func (a *App) setupModels(cfg *config.Config) error {
	oc := onnx.Config{LibraryPath: cfg.Tier1.LibraryPath, PoolSize: cfg.Tier1.PoolSize, Threads: cfg.Tier1.Threads, MaxLen: cfg.Tier1.MaxLen, Stride: cfg.Tier1.Stride, MaxWindows: cfg.Tier1.MaxWindows, InjectionIndex: cfg.Tier1.InjectionIndex, NERThreshold: cfg.Tier1.NERThreshold, Temperature: cfg.Tier1.Temperature}
	if cfg.Tier1.Enabled && cfg.Tier1.ClassifierPath == "" && cfg.Tier1.Model != "" {
		m, ok := models.Find(cfg.Tier1.Model)
		if !ok || m.Kind != models.KindClassifier || !m.Local {
			return fmt.Errorf("tier1: %q is not a downloadable classifier, see: rungated models list", cfg.Tier1.Model)
		}
		f, err := models.ModelFile("", m.ID)
		if err != nil {
			return err
		}
		cfg.Tier1.ClassifierPath = f
	}
	if cfg.Tier1.Enabled {
		oc.ClassifierPath, oc.NERPath, oc.NERLabelsPath = cfg.Tier1.ClassifierPath, cfg.Tier1.NERPath, cfg.Tier1.NERLabelsPath
		oc.TokenizerPath, oc.NERTokenizerPath = cfg.Tier1.TokenizerPath, cfg.Tier1.NERTokenizerPath
	}
	if cfg.Embed.Enabled {
		mp, tp, err := embedPaths(cfg)
		if err != nil {
			return err
		}
		oc.EmbedPath, oc.EmbedTokenizerPath, oc.EmbedName = mp, tp, "onnx."+cfg.Embed.Model
		if cfg.Embed.Model == "" {
			oc.EmbedName = "onnx.embed"
		}
		if cfg.Embed.MaxLen > 0 && cfg.Tier1.MaxLen == 0 {
			oc.MaxLen = cfg.Embed.MaxLen
		}
	}
	var local []detect.Classifier
	var localNER detect.NER
	if oc.ClassifierPath != "" || oc.NERPath != "" || oc.EmbedPath != "" {
		m, err := onnx.Load(oc)
		if err != nil {
			return fmt.Errorf("local models: %w", err)
		}
		a.closers = append(a.closers, m.Close)
		if m.Classifier != nil {
			local = append(local, m.Classifier)
		}
		if m.Embedder != nil {
			s, err := a.similarity(m.Embedder)
			if err != nil {
				return err
			}
			local = append(local, s)
		}
		localNER = m.NER
		slog.Info("local models loaded", "classifier", oc.ClassifierPath, "ner", oc.NERPath, "embedder", oc.EmbedPath)
	}
	backup := combine(local)
	a.Detect.Classifier, a.Detect.NER = backup, localNER
	if cfg.Tier1.Threshold > 0 {
		a.Detect.ClassifierThreshold = cfg.Tier1.Threshold
	}
	if cfg.Tier1.TimeoutMS > 0 {
		a.Detect.Tier1Timeout = time.Duration(cfg.Tier1.TimeoutMS) * time.Millisecond
	}
	if !cfg.Cloud.Enabled() {
		return nil
	}
	if err := checkCloudModel(cfg.Cloud.Classifier, models.KindClassifier); err != nil {
		return err
	}
	if err := checkCloudModel(cfg.Cloud.NER, models.KindNER); err != nil {
		return err
	}
	if err := checkCloudModel(cfg.Cloud.Embedder, models.KindEmbed); err != nil {
		return err
	}
	timeout := time.Duration(cfg.Cloud.TimeoutMS) * time.Millisecond
	c := cloud.New(cfg.Cloud.URL, cfg.Cloud.APIKey, timeout)
	c.Version = a.Version
	var remote []detect.Classifier
	if cfg.Cloud.Classifier != "" {
		remote = append(remote, &cloud.Classifier{C: c, Model: cfg.Cloud.Classifier})
	}
	if cfg.Cloud.Embedder != "" {
		s, err := a.similarity(&cloud.Embedder{C: c, Model: cfg.Cloud.Embedder})
		if err != nil {
			return err
		}
		remote = append(remote, s)
	}
	ended := func(err error) bool { return errors.Is(err, cloud.ErrTrialEnded) }
	if p := combine(remote); p != nil {
		if backup == nil {
			p = &detect.QuietClassifier{C: p, Quiet: ended}
		}
		a.Detect.Classifier = &detect.FallbackClassifier{Primary: p, Backup: backup}
	}
	if cfg.Cloud.NER != "" {
		var n detect.NER = &cloud.NER{C: c, Model: cfg.Cloud.NER}
		if localNER == nil {
			n = &detect.QuietNER{N: n, Quiet: ended}
		}
		a.Detect.NER = &detect.FallbackNER{Primary: n, Backup: localNER}
	}
	if a.Detect.Tier1Timeout < timeout+50*time.Millisecond {
		a.Detect.Tier1Timeout = timeout + 50*time.Millisecond
	}
	slog.Info("hosted models enabled", "url", cfg.Cloud.URL, "classifier", cfg.Cloud.Classifier, "ner", cfg.Cloud.NER, "embedder", cfg.Cloud.Embedder)
	return nil
}
