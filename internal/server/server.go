package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amareshhebbar/rungated/internal/api"
	"github.com/amareshhebbar/rungated/internal/approvals"
	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/internal/consoleui"
	"github.com/amareshhebbar/rungated/internal/contain"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/egress"
	"github.com/amareshhebbar/rungated/internal/events"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/llmproxy"
	"github.com/amareshhebbar/rungated/internal/mcpproxy"
	"github.com/amareshhebbar/rungated/internal/metrics"
	"github.com/amareshhebbar/rungated/internal/policy"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/internal/vault"
)

type App struct {
	Cfg       *config.Config
	Version   string
	Store     store.Store
	Bus       events.Bus
	Pipeline  *events.Pipeline
	Policy    *policy.Engine
	Detect    *detect.Engine
	Vault     *vault.Vault
	Approvals *approvals.Service
	Contain   *contain.Service
	Registry  *gateway.Registry
	Engine    *gateway.Engine
	Resolver  *auth.Resolver
	Keys      *auth.APIKeys
	Metrics   *metrics.Metrics
	LLM       *llmproxy.Proxy
	MCP       *mcpproxy.Proxy
	Egress    *egress.Proxy
	API       *api.API
	Ext       map[string]any
	Static    http.Handler
	Mux       http.Handler

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closers []func()
}

func derive(master []byte, label string) []byte {
	m := hmac.New(sha256.New, master)
	m.Write([]byte("rungated:" + label))
	return m.Sum(nil)
}

func New(ctx context.Context, cfg *config.Config, version string) (*App, error) {
	a := &App{Cfg: cfg, Version: version, Metrics: metrics.New(), Ext: map[string]any{}}
	bg, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()

	if err := checkKeys(cfg); err != nil {
		return nil, err
	}
	if edition.Name == "free" {
		for _, f := range enterpriseRequested(cfg) {
			needsEnterprise(f)
		}
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	master, err := cfg.MasterKey()
	if err != nil {
		return nil, err
	}
	a.Vault, err = vault.New(derive(master, "vault"))
	if err != nil {
		return nil, err
	}

	switch cfg.Store.Kind {
	case "postgres":
		pg, err := store.NewPostgres(ctx, cfg.Store.DSN)
		if err != nil {
			return nil, err
		}
		a.Store = pg
	default:
		ms, err := store.NewMemStore(filepath.Join(cfg.DataDir, "state"), cfg.Store.MemoryEvents)
		if err != nil {
			return nil, err
		}
		a.Store = ms
	}
	a.closers = append(a.closers, func() { a.Store.Close() })

	if edition.Bus != nil {
		b, err := edition.Bus(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if b != nil {
			a.Bus = b
		}
	}
	if a.Bus == nil {
		a.Bus = events.NewLocal(65536)
	}

	auditor, err := events.NewAuditor(ctx, a.Store)
	if err != nil {
		return nil, err
	}
	var sinks []events.Sink
	if edition.Sinks != nil {
		sinks = append(sinks, edition.Sinks(ctx, cfg)...)
	}
	for _, w := range cfg.Webhooks {
		sinks = append(sinks, &events.Webhook{URL: w.URL, Secret: w.Secret, Format: w.Format, Token: w.Token, MinVerdict: w.MinVerdict})
	}
	a.Pipeline = &events.Pipeline{Bus: a.Bus, Store: a.Store, Auditor: auditor, Sinks: sinks}
	a.Pipeline.Start(bg)
	a.closers = append(a.closers, func() { a.Pipeline.Stop(); a.Bus.Close() })

	a.Policy = policy.NewEngine()
	a.Detect = detect.NewEngine()
	if err := a.setupModels(cfg); err != nil {
		return nil, err
	}
	var judge detect.Judge
	if edition.Judge != nil {
		judge = edition.Judge(cfg)
	}

	issuer := contain.NewIssuer(derive(master, "rgt1"), time.Duration(cfg.TokenTTLSecs)*time.Second)
	var snaps contain.SnapshotStore
	switch cfg.Snapshots.Kind {
	case "none":
	case "s3":
		if edition.Snapshots != nil {
			snaps = edition.Snapshots(cfg)
		}
		if snaps == nil {
			snaps = &contain.FileSnapshots{Dir: cfg.Snapshots.Dir}
		}
	default:
		snaps = &contain.FileSnapshots{Dir: cfg.Snapshots.Dir}
	}
	a.Contain, err = contain.New(ctx, a.Store, issuer, snaps, a.Bus.Publish)
	if err != nil {
		return nil, err
	}
	a.Approvals = approvals.New(a.Store, a.Bus.Publish)
	if edition.Notifiers != nil {
		edition.Notifiers(a)
	}
	go a.Approvals.Run(bg)

	a.Registry = gateway.NewRegistry(a.Store, a.Policy)
	if err := a.Registry.LoadAll(ctx); err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}
	a.Engine = &gateway.Engine{
		Registry: a.Registry, Policy: a.Policy, Detect: a.Detect, Judge: judge, Vault: a.Vault, Approvals: a.Approvals,
		Contain: a.Contain, Bus: a.Bus, Store: a.Store, Incidents: gateway.NewIncidents(a.Store), Stats: gateway.NewStats(),
		UnknownAgents: cfg.UnknownAgents,
	}
	a.Engine.Observers = append(a.Engine.Observers, a.Metrics.ObserveDecision)
	a.Contain.OnKill(func(tenant, agent, session string) { a.Vault.Purge(tenant, session) })
	a.Metrics.Gauge("rungated_active_containments", func() float64 { return float64(len(a.Contain.Active())) })
	a.Metrics.Gauge("rungated_registered_agents", func() float64 { return float64(len(a.Registry.List(""))) })
	a.Metrics.Gauge("rungated_vault_sessions", func() float64 { return float64(a.Vault.Stats()["sessions"]) })
	a.Metrics.Gauge("rungated_vault_tokens", func() float64 { return float64(a.Vault.Stats()["tokens"]) })

	keys := &auth.APIKeys{Keys: map[string]auth.Principal{}, Disabled: cfg.Auth.DisableConsoleAuth || cfg.Dev}
	for _, k := range cfg.Auth.APIKeys {
		keys.Keys[k.Key] = auth.Principal{Name: k.Name, Role: auth.Role(k.Role), Tenant: k.Tenant}
	}
	if !keys.Disabled && len(keys.Keys) == 0 {
		return nil, errors.New("no api keys configured: set RUNGATED_API_KEY, add auth.api_keys, or run with --dev")
	}
	a.Keys = keys
	a.Resolver = &auth.Resolver{Issuer: issuer, Contain: a.Contain, Keys: a.Registry, RequireAuth: cfg.Auth.RequireAgentAuth && !cfg.Dev}

	prices := map[string]llmproxy.Price{}
	for k, v := range cfg.Prices {
		prices[k] = llmproxy.Price{In: v.InputPer1M, Out: v.OutputPer1M}
	}
	extra := map[string]llmproxy.Upstream{}
	for k, v := range cfg.Upstreams.Extra {
		extra[k] = llmproxy.Upstream{BaseURL: v.BaseURL, APIKey: v.APIKey}
	}
	a.LLM = llmproxy.New(a.Engine, a.Resolver, llmproxy.Config{
		OpenAI:    llmproxy.Upstream{BaseURL: cfg.Upstreams.OpenAI.BaseURL, APIKey: cfg.Upstreams.OpenAI.APIKey},
		Anthropic: llmproxy.Upstream{BaseURL: cfg.Upstreams.Anthropic.BaseURL, APIKey: cfg.Upstreams.Anthropic.APIKey},
		Extra:     extra, Prices: prices, MaxBody: int64(cfg.MaxBodyMB) << 20,
	}, a.Metrics)

	a.MCP = mcpproxy.NewProxy(a.Engine, a.Resolver, mcpproxy.New(a.Engine), int64(cfg.MaxBodyMB)<<20)

	a.Egress, err = egress.New(a.Engine, a.Resolver, egress.Config{
		InterceptTLS: cfg.Egress.InterceptTLS, CADir: filepath.Join(cfg.DataDir, "egress"),
		MaxScanBytes: int64(cfg.Egress.MaxScanKB) << 10, ParentProxy: cfg.Egress.ParentProxy,
	})
	if err != nil {
		return nil, fmt.Errorf("egress proxy: %w", err)
	}

	for _, p := range cfg.Manifests {
		if err := a.registerManifests(ctx, p); err != nil {
			return nil, err
		}
	}

	if a.Static == nil && consoleui.Available() {
		a.Static = consoleui.Handler()
	}
	a.API = api.New(api.Deps{
		Version: version, Registry: a.Registry, Engine: a.Engine, Policy: a.Policy, Store: a.Store, Bus: a.Bus, Approvals: a.Approvals,
		Contain: a.Contain, Vault: a.Vault, Keys: keys, Resolver: a.Resolver, Metrics: a.Metrics, Edition: edition.Name, Ext: a.Ext, CORS: cfg.CORSOrigins, Static: a.Static,
	})
	mux := http.NewServeMux()
	mux.Handle("/llm/", a.LLM)
	mux.Handle("/mcp/", a.MCP)
	mux.HandleFunc("POST /v1/mcp/{server}/intercept", a.MCP.Intercept)
	if a.Egress.CA != nil {
		mux.HandleFunc("GET /v1/egress/ca.pem", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Write(a.Egress.CA.PEM())
		})
	}
	mux.Handle("/", a.API.Handler())
	a.Mux = logging(mux)

	go a.gc(bg)
	ok = true
	return a, nil
}

func (a *App) registerManifests(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("manifest path %s: %w", path, err)
	}
	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, e := range entries {
			n := strings.ToLower(e.Name())
			if !e.IsDir() && (strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".json")) {
				files = append(files, filepath.Join(path, e.Name()))
			}
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		res, err := a.Registry.Register(ctx, raw, false)
		if err != nil {
			return fmt.Errorf("manifest %s: %w", f, err)
		}
		if res.Created {
			slog.Info("registered agent", "id", res.Agent.Manifest.Metadata.ID, "tenant", res.Agent.Manifest.Metadata.Tenant, "key", res.Key)
		} else {
			slog.Info("updated agent", "id", res.Agent.Manifest.Metadata.ID, "version", res.Agent.Record.Version)
		}
	}
	return nil
}

func (a *App) gc(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Vault.GC()
			if a.Detect.Limiter != nil {
				a.Detect.Limiter.GC(2 * time.Hour)
			}
			if a.Engine != nil && a.Engine.Incidents != nil {
				a.Engine.Incidents.GC(time.Now().UTC())
			}
		}
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(c int) {
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/v1/health" || r.URL.Path == "/metrics" {
			return
		}
		slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status, "ms", time.Since(start).Milliseconds(), "agent", r.Header.Get("X-Rungated-Agent"))
	})
}

func (a *App) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr: a.Cfg.Listen, Handler: a.Mux,
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errc := make(chan error, 2)
	go func() { errc <- srv.ListenAndServe() }()
	var esrv *http.Server
	if a.Cfg.EgressListen != "" {
		esrv = &http.Server{Addr: a.Cfg.EgressListen, Handler: a.Egress, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second}
		go func() { errc <- esrv.ListenAndServe() }()
		slog.Info("rungated egress proxy listening", "addr", a.Cfg.EgressListen, "tls_intercept", a.Cfg.Egress.InterceptTLS)
	}
	slog.Info("rungated gateway listening", "addr", a.Cfg.Listen, "version", a.Version, "edition", edition.Name, "store", a.Cfg.Store.Kind, "bus", a.Cfg.Bus.Kind, "dev", a.Cfg.Dev)
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
		if esrv != nil {
			esrv.Shutdown(sctx)
		}
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	for i := len(a.closers) - 1; i >= 0; i-- {
		a.closers[i]()
	}
	a.closers = nil
}
