package detect

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

type Embedder interface {
	Name() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type BankEntry struct {
	Text  string `json:"text"`
	Label int    `json:"label"`
	Head  string `json:"head,omitempty"`
}

//go:embed bank.jsonl
var defaultBank []byte

func LoadBank(path string) ([]BankEntry, error) {
	raw := defaultBank
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	var out []BankEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e BankEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("bank line %d: %w", n, err)
		}
		if e.Text != "" {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

type Similarity struct {
	Emb       Embedder
	Bank      []BankEntry
	Threshold float64
	Sharpness float64
	ChunkLen  int
	MaxChunks int

	mu      sync.Mutex
	ready   bool
	retry   time.Time
	lastErr error
	attacks [][]float32
	benign  [][]float32
}

func (s *Similarity) Name() string { return "similarity." + s.Emb.Name() }

func (s *Similarity) prepare(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready {
		return nil
	}
	if time.Now().Before(s.retry) && s.lastErr != nil {
		return s.lastErr
	}
	err := s.build(ctx)
	if err != nil {
		s.retry = time.Now().Add(30 * time.Second)
		s.lastErr = err
		return err
	}
	s.ready = true
	return nil
}

func (s *Similarity) build(ctx context.Context) error {
	if len(s.Bank) == 0 {
		b, err := LoadBank("")
		if err != nil {
			return err
		}
		s.Bank = b
	}
	texts := make([]string, len(s.Bank))
	for i, e := range s.Bank {
		texts[i] = e.Text
	}
	vecs, err := s.Emb.Embed(ctx, texts)
	if err != nil {
		return err
	}
	if len(vecs) != len(texts) {
		return errors.New("embedder returned wrong number of vectors")
	}
	var att, ben [][]float32
	for i, e := range s.Bank {
		v := normalize(vecs[i])
		if e.Label == 1 {
			att = append(att, v)
		} else {
			ben = append(ben, v)
		}
	}
	if len(att) == 0 {
		return errors.New("similarity bank has no attack examples")
	}
	s.attacks, s.benign = att, ben
	return nil
}

func (s *Similarity) Prepare(ctx context.Context) error { return s.prepare(ctx) }

func (s *Similarity) Classify(ctx context.Context, texts []string) ([]float64, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	chunkLen, maxChunks := s.ChunkLen, s.MaxChunks
	if chunkLen <= 0 {
		chunkLen = 600
	}
	if maxChunks <= 0 {
		maxChunks = 8
	}
	var flat []string
	var owner []int
	for i, t := range texts {
		for _, c := range Chunks(t, chunkLen, maxChunks) {
			flat = append(flat, c)
			owner = append(owner, i)
		}
	}
	out := make([]float64, len(texts))
	if len(flat) == 0 {
		return out, nil
	}
	vecs, err := s.Emb.Embed(ctx, flat)
	if err != nil {
		return nil, err
	}
	th, k := s.Threshold, s.Sharpness
	if th <= 0 {
		th = 0.72
	}
	if k <= 0 {
		k = 25
	}
	for j, v := range vecs {
		v = normalize(v)
		a := maxCos(v, s.attacks)
		b := maxCos(v, s.benign)
		score := a
		if b > a {
			score = a - (b-a)/2
		}
		p := 1 / (1 + math.Exp(-k*(score-th)))
		if p > out[owner[j]] {
			out[owner[j]] = p
		}
	}
	return out, nil
}

func Chunks(text string, size, max int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= size {
		return []string{text}
	}
	var out []string
	r := []rune(text)
	step := size * 3 / 4
	for start := 0; start < len(r); start += step {
		end := start + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[start:end]))
		if end == len(r) {
			break
		}
	}
	if max > 0 && len(out) > max {
		last := out[len(out)-1]
		out = append(out[:max-1:max-1], last)
	}
	return out
}

func normalize(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

func maxCos(v []float32, set [][]float32) float64 {
	best := -1.0
	for _, u := range set {
		if len(u) != len(v) {
			continue
		}
		var d float64
		for i := range v {
			d += float64(v[i]) * float64(u[i])
		}
		if d > best {
			best = d
		}
	}
	return best
}

type MaxClassifier struct {
	List []Classifier
}

func (m *MaxClassifier) Name() string {
	names := make([]string, len(m.List))
	for i, c := range m.List {
		names[i] = c.Name()
	}
	return strings.Join(names, "+")
}

func (m *MaxClassifier) Classify(ctx context.Context, texts []string) ([]float64, error) {
	out := make([]float64, len(texts))
	var errs []error
	ok := 0
	for _, c := range m.List {
		p, err := c.Classify(ctx, texts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Name(), err))
			continue
		}
		ok++
		for i := range out {
			if i < len(p) && p[i] > out[i] {
				out[i] = p[i]
			}
		}
	}
	if ok == 0 && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}

type QuietClassifier struct {
	C     Classifier
	Quiet func(error) bool
}

func (q *QuietClassifier) Name() string { return q.C.Name() }

func (q *QuietClassifier) Classify(ctx context.Context, texts []string) ([]float64, error) {
	p, err := q.C.Classify(ctx, texts)
	if err != nil && q.Quiet(err) {
		return make([]float64, len(texts)), nil
	}
	return p, err
}

type QuietNER struct {
	N     NER
	Quiet func(error) bool
}

func (q *QuietNER) Name() string { return q.N.Name() }

func (q *QuietNER) Extract(ctx context.Context, text string) ([]Entity, error) {
	e, err := q.N.Extract(ctx, text)
	if err != nil && q.Quiet(err) {
		return nil, nil
	}
	return e, err
}

type FallbackClassifier struct {
	Primary Classifier
	Backup  Classifier
}

func (f *FallbackClassifier) Name() string { return f.Primary.Name() }

func (f *FallbackClassifier) Classify(ctx context.Context, texts []string) ([]float64, error) {
	p, err := f.Primary.Classify(ctx, texts)
	if err == nil || f.Backup == nil {
		return p, err
	}
	return f.Backup.Classify(ctx, texts)
}

type FallbackNER struct {
	Primary NER
	Backup  NER
}

func (f *FallbackNER) Name() string { return f.Primary.Name() }

func (f *FallbackNER) Extract(ctx context.Context, text string) ([]Entity, error) {
	e, err := f.Primary.Extract(ctx, text)
	if err == nil || f.Backup == nil {
		return e, err
	}
	return f.Backup.Extract(ctx, text)
}
