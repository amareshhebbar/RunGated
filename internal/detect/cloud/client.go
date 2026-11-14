package cloud

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/detect"
)

var ErrTrialEnded = errors.New("trial ended")

type TrialEndedError struct {
	Model string
}

func (e *TrialEndedError) Error() string { return e.Model + ": trial ended" }

func (e *TrialEndedError) Unwrap() error { return ErrTrialEnded }

type Client struct {
	URL     string
	APIKey  string
	Timeout time.Duration
	HTTP    *http.Client
	Version string

	mu     sync.Mutex
	ended  map[string]time.Time
	warned map[string]time.Time
}

func New(url, key string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 400 * time.Millisecond
	}
	return &Client{URL: strings.TrimRight(url, "/"), APIKey: key, Timeout: timeout, ended: map[string]time.Time{}, warned: map[string]time.Time{}}
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type apiError struct {
	Error string `json:"error"`
	Model string `json:"model"`
}

func (c *Client) post(ctx context.Context, path, model string, body, out any) error {
	c.mu.Lock()
	until, ended := c.ended[model]
	c.mu.Unlock()
	if ended && time.Now().Before(until) {
		return &TrialEndedError{Model: model}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	if c.Version != "" {
		req.Header.Set("User-Agent", "rungated/"+c.Version)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	c.noteDays(model, resp.Header.Get("X-Rungated-Trial-Days-Left"))
	if resp.StatusCode == http.StatusPaymentRequired {
		c.mu.Lock()
		c.ended[model] = time.Now().Add(6 * time.Hour)
		c.mu.Unlock()
		slog.Warn("hosted model trial ended, using local models", "model", model, "contact", "hebbar.gvamaresh@gmail.com")
		return &TrialEndedError{Model: model}
	}
	if resp.StatusCode != http.StatusOK {
		var e apiError
		json.Unmarshal(data, &e)
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(data))
		}
		return fmt.Errorf("rungated cloud %s: HTTP %d: %s", path, resp.StatusCode, e.Error)
	}
	return json.Unmarshal(data, out)
}

func (c *Client) noteDays(model, v string) {
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 3 {
		return
	}
	c.mu.Lock()
	last := c.warned[model]
	if time.Since(last) < 24*time.Hour {
		c.mu.Unlock()
		return
	}
	c.warned[model] = time.Now()
	c.mu.Unlock()
	slog.Warn("hosted model trial ends soon", "model", model, "days_left", n)
}

type Entitlement struct {
	ID       string    `json:"id"`
	Tier     string    `json:"tier"`
	Active   bool      `json:"active"`
	DaysLeft int       `json:"days_left"`
	Started  time.Time `json:"started,omitempty"`
}

type Entitlements struct {
	Plan   string        `json:"plan"`
	Owner  string        `json:"owner"`
	Models []Entitlement `json:"models"`
}

func (c *Client) Entitlements(ctx context.Context) (*Entitlements, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/v1/entitlements", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var e Entitlements
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

type Classifier struct {
	C     *Client
	Model string
}

func (k *Classifier) Name() string { return "cloud." + k.Model }

func (k *Classifier) Classify(ctx context.Context, texts []string) ([]float64, error) {
	var out struct {
		Scores []float64 `json:"scores"`
	}
	if err := k.C.post(ctx, "/v1/classify", k.Model, map[string]any{"model": k.Model, "texts": texts}, &out); err != nil {
		return nil, err
	}
	if len(out.Scores) != len(texts) {
		return nil, errors.New("rungated cloud: score count mismatch")
	}
	return out.Scores, nil
}

type NER struct {
	C     *Client
	Model string
}

func (k *NER) Name() string { return "cloud." + k.Model }

func (k *NER) Extract(ctx context.Context, text string) ([]detect.Entity, error) {
	var out struct {
		Entities []struct {
			Label string  `json:"label"`
			Start int     `json:"start"`
			End   int     `json:"end"`
			Score float64 `json:"score"`
		} `json:"entities"`
	}
	if err := k.C.post(ctx, "/v1/ner", k.Model, map[string]any{"model": k.Model, "text": text}, &out); err != nil {
		return nil, err
	}
	ents := make([]detect.Entity, 0, len(out.Entities))
	for _, e := range out.Entities {
		if e.Start < 0 || e.End > len(text) || e.Start >= e.End {
			continue
		}
		ents = append(ents, detect.Entity{Label: e.Label, Start: e.Start, End: e.End, Score: e.Score, Text: text[e.Start:e.End]})
	}
	return ents, nil
}

type Embedder struct {
	C     *Client
	Model string
}

func (k *Embedder) Name() string { return "cloud." + k.Model }

func (k *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	var out struct {
		Vectors [][]float32 `json:"vectors"`
	}
	for start := 0; start < len(texts); start += 64 {
		end := start + 64
		if end > len(texts) {
			end = len(texts)
		}
		var part struct {
			Vectors [][]float32 `json:"vectors"`
		}
		if err := k.C.post(ctx, "/v1/embed", k.Model, map[string]any{"model": k.Model, "texts": texts[start:end]}, &part); err != nil {
			return nil, err
		}
		if len(part.Vectors) != end-start {
			return nil, errors.New("rungated cloud: vector count mismatch")
		}
		out.Vectors = append(out.Vectors, part.Vectors...)
	}
	return out.Vectors, nil
}
