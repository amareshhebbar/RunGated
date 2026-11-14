package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type File struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type Manifest struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
	License string `json:"license"`
	Files   []File `json:"files"`
}

const ManifestName = "rungated.json"

var HubURL = "https://huggingface.co"

func DefaultDir() string {
	if v := os.Getenv("RUNGATED_MODELS_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "rungated", "models")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "models")
	}
	return filepath.Join(home, ".local", "share", "rungated", "models")
}

func Path(dir, id string) string {
	if dir == "" {
		dir = DefaultDir()
	}
	return filepath.Join(dir, id)
}

func Installed(dir, id string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(Path(dir, id), ManifestName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func ModelFile(dir, id string) (string, error) {
	m, err := Installed(dir, id)
	if err != nil {
		return "", fmt.Errorf("model %s is not installed, run: rungated models pull %s", id, id)
	}
	var pick string
	for _, f := range m.Files {
		if strings.HasSuffix(f.Name, ".int8.onnx") {
			pick = f.Name
		}
	}
	if pick == "" {
		for _, f := range m.Files {
			if strings.HasSuffix(f.Name, ".onnx") {
				pick = f.Name
				break
			}
		}
	}
	if pick == "" {
		return "", fmt.Errorf("model %s has no onnx file", id)
	}
	return filepath.Join(Path(dir, id), pick), nil
}

type Puller struct {
	Client *http.Client
	Token  string
	Log    io.Writer
}

func (p *Puller) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Minute}
}

func (p *Puller) get(ctx context.Context, repo, name string) (*http.Response, error) {
	u := fmt.Sprintf("%s/%s/resolve/main/%s", strings.TrimRight(HubURL, "/"), repo, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	req.Header.Set("User-Agent", "rungated-models")
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return resp, nil
}

func (p *Puller) Pull(ctx context.Context, id, dir string) (string, error) {
	m, ok := Find(id)
	if !ok {
		return "", fmt.Errorf("unknown model %q, see: rungated models list", id)
	}
	if !m.Local {
		return "", fmt.Errorf("%s is hosted only (%s): set cloud.api_key and cloud.%s: %s in rungated.yaml", id, m.Tier.Label(), cloudField(m.Kind), id)
	}
	resp, err := p.get(ctx, m.Repo, ManifestName)
	if err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return "", fmt.Errorf("bad %s: %w", ManifestName, err)
	}
	if len(man.Files) == 0 {
		return "", errors.New("model manifest lists no files")
	}
	dst := Path(dir, id)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", err
	}
	for _, f := range man.Files {
		if strings.Contains(f.Name, "..") || strings.HasPrefix(f.Name, "/") {
			return "", fmt.Errorf("bad file name %q", f.Name)
		}
		target := filepath.Join(dst, f.Name)
		if f.SHA256 != "" && fileSHA(target) == f.SHA256 {
			p.logf("%s up to date\n", f.Name)
			continue
		}
		p.logf("downloading %s\n", f.Name)
		if err := p.download(ctx, m.Repo, f, target); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(dst, ManifestName), raw, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func (p *Puller) download(ctx context.Context, repo string, f File, target string) error {
	resp, err := p.get(ctx, repo, f.Name)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, h), resp.Body)
	out.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if f.SHA256 != "" && hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		os.Remove(tmp)
		return fmt.Errorf("%s: checksum mismatch", f.Name)
	}
	return os.Rename(tmp, target)
}

func (p *Puller) logf(format string, a ...any) {
	if p.Log != nil {
		fmt.Fprintf(p.Log, format, a...)
	}
}

func fileSHA(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func cloudField(k Kind) string {
	switch k {
	case KindNER:
		return "ner"
	case KindEmbed:
		return "embedder"
	case KindJudge:
		return "judge"
	}
	return "classifier"
}
