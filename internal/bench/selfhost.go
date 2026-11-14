package bench

import (
	"context"
	_ "embed"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/internal/server"
)

//go:embed agent.yaml
var AgentManifest []byte

const AgentID = "bench-agent"

type SelfHosted struct {
	URL     string
	Tier1   bool
	srv     *httptest.Server
	app     *server.App
	cleanup func()
}

func (s *SelfHosted) Close() {
	s.srv.Close()
	s.app.Close()
	s.cleanup()
}

func StartSelfHosted(ctx context.Context, cfgPath, version string) (*SelfHosted, error) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	dir, err := os.MkdirTemp("", "rungated-bench-")
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	cfg.Dev = true
	cfg.DataDir = dir
	cfg.MasterKeyFile = ""
	cfg.Snapshots.Dir = filepath.Join(dir, "snapshots")
	cfg.EgressListen = ""
	cfg.Manifests = nil
	cfg.Store.Kind = "embedded"
	cfg.Bus.Kind = "local"
	if err := cfg.Finalize(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	app, err := server.New(ctx, cfg, version)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if _, err := app.Registry.Register(ctx, AgentManifest, false); err != nil {
		app.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	srv := httptest.NewServer(app.Mux)
	return &SelfHosted{URL: srv.URL, Tier1: cfg.Tier1.Enabled, srv: srv, app: app, cleanup: func() { os.RemoveAll(dir) }}, nil
}
