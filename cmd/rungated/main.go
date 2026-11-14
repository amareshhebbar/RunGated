package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/amareshhebbar/rungated/internal/bench"
	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/internal/demo"
	"github.com/amareshhebbar/rungated/internal/detect/onnx"
	"github.com/amareshhebbar/rungated/internal/events"
	"github.com/amareshhebbar/rungated/internal/mcpproxy"
	"github.com/amareshhebbar/rungated/internal/policy"
	"github.com/amareshhebbar/rungated/internal/server"
	"github.com/amareshhebbar/rungated/internal/store"
	"github.com/amareshhebbar/rungated/pkg/manifest"
)

var version = "0.10.0"

func usage() {
	fmt.Fprintln(os.Stderr, `rungated <command>

commands:
  serve [--config F] [--listen :8080] [--data DIR] [--dev] [--manifests PATHS]
                                       run the gateway, control API and LLM proxy
  init --id ID [--owner E] [--purpose P] [--out F]
                                       write a starter rungated.agent.yaml
  init --id ID --from-gateway URL --api-key K
                                       write a manifest learned from the agent's traffic so far
  init --id ID --from-events events.jsonl
                                       same, from an events file in the data directory
  validate <rungated.agent.yaml>       validate an agent manifest
  compile-policy <rungated.agent.yaml> print the compiled Cedar policy
  token --gateway URL --agent ID (--key K | --api-key K) [--session S] [--ttl N]
                                       request an agent token from a running gateway
  agents register <file> --gateway URL --api-key K [--rotate-key]
  agents list --gateway URL --api-key K
  audit verify [--gateway URL --api-key K | --config F | --data DIR]
                                       verify the hash chained audit log
  mcp-stdio --gateway URL --server NAME --agent ID (--key K | --token T) [--session S] -- command args...
                                       wrap a stdio MCP server so every message passes through the gateway
  tier1 bench --classifier F [--ner F] [--tokenizer F] [--ner-tokenizer F] [--ner-labels F] [--lib F] [--iters N] [--pool N] [--threads N] [--max-len N]
                                       measure Tier 1 model latency and sanity check outputs (needs the onnx build tag)
  bench gen|run                        attack corpus generator and benchmark runner
  demo <mock-llm|poisoned-mcp|evil-web> [--listen ADDR]
                                       run a demo service used by the attack demo
  models list|pull ID [--dir D]|path ID
                                       list the Rungated models, download a free model
  version                              print version
  edition                              print the edition, free or enterprise`)
	os.Exit(2)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "edition":
		fmt.Println(server.EditionName())
	case "models":
		modelsCmd(os.Args[2:])
	case "init":
		initCmd(os.Args[2:])
	case "validate":
		if len(os.Args) < 3 {
			usage()
		}
		m, err := manifest.Load(os.Args[2])
		if err != nil {
			if ve, ok := err.(*manifest.ValidationError); ok {
				json.NewEncoder(os.Stdout).Encode(ve)
			} else {
				fmt.Fprintln(os.Stderr, err)
			}
			os.Exit(1)
		}
		fmt.Printf("ok: agent %s (tenant %s, %d tools, mode %s)\n", m.Metadata.ID, m.Metadata.Tenant, len(m.Spec.Tools), m.Spec.Autonomy.Default)
	case "compile-policy":
		if len(os.Args) < 3 {
			usage()
		}
		m, err := manifest.Load(os.Args[2])
		if err != nil {
			fatal(err)
		}
		src, err := policy.Compile(m)
		if err != nil {
			fatal(err)
		}
		fmt.Print(src)
	case "serve":
		serve(os.Args[2:])
	case "token":
		tokenCmd(os.Args[2:])
	case "agents":
		agentsCmd(os.Args[2:])
	case "audit":
		auditCmd(os.Args[2:])
	case "mcp-stdio":
		mcpStdioCmd(os.Args[2:])
	case "tier1":
		tier1Cmd(os.Args[2:])
	case "bench":
		if err := bench.Main(os.Args[2:], version, os.Stdout); err != nil {
			fatal(err)
		}
	case "demo":
		if err := demo.Run(os.Args[2:]); err != nil {
			fatal(err)
		}
	default:
		usage()
	}
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "", "config file")
	listen := fs.String("listen", "", "listen address")
	data := fs.String("data", "", "data directory")
	dev := fs.Bool("dev", false, "development mode: no console auth, header identity, registers spec/ats/examples")
	manifests := fs.String("manifests", "", "comma separated manifest files or directories to register at startup")
	fs.Parse(args)
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	if *data != "" {
		cfg.DataDir = *data
		cfg.MasterKeyFile = ""
		cfg.Snapshots.Dir = ""
	}
	if *dev {
		cfg.Dev = true
		if len(cfg.Manifests) == 0 && *manifests == "" {
			if _, err := os.Stat("spec/ats/examples"); err == nil {
				cfg.Manifests = []string{"spec/ats/examples"}
			}
		}
	}
	if *manifests != "" {
		cfg.Manifests = append(cfg.Manifests, strings.Split(*manifests, ",")...)
	}
	if err := cfg.Finalize(); err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app, err := server.New(ctx, cfg, version)
	if err != nil {
		fatal(err)
	}
	defer app.Close()
	if err := app.Run(ctx); err != nil {
		fatal(err)
	}
}

func doJSON(method, u, apiKey string, extra map[string]string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	c := &http.Client{Timeout: 30 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b, nil
}

func pretty(b []byte) string {
	var out bytes.Buffer
	if json.Indent(&out, b, "", "  ") == nil {
		return out.String()
	}
	return string(b)
}

func join(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func tokenCmd(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	gw := fs.String("gateway", "http://localhost:8080", "gateway base URL")
	agent := fs.String("agent", "", "agent id")
	key := fs.String("key", "", "agent key")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "admin api key")
	session := fs.String("session", "", "session id")
	ttl := fs.Int("ttl", 0, "ttl in seconds")
	fs.Parse(args)
	if *agent == "" {
		fatal(fmt.Errorf("--agent is required"))
	}
	extra := map[string]string{}
	if *key != "" {
		extra["X-Rungated-Agent"] = *agent
		extra["X-Rungated-Key"] = *key
	}
	code, b, err := doJSON("POST", join(*gw, "/v1/agents/"+url.PathEscape(*agent)+"/token"), *apiKey, extra, map[string]any{"session_id": *session, "ttl_seconds": *ttl})
	if err != nil {
		fatal(err)
	}
	if code != 200 {
		fatal(fmt.Errorf("gateway returned %d: %s", code, strings.TrimSpace(string(b))))
	}
	var out struct {
		Token string `json:"token"`
	}
	json.Unmarshal(b, &out)
	fmt.Println(out.Token)
}

func agentsCmd(args []string) {
	if len(args) == 0 {
		usage()
	}
	fs := flag.NewFlagSet("agents", flag.ExitOnError)
	gw := fs.String("gateway", "http://localhost:8080", "gateway base URL")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "api key")
	rotate := fs.Bool("rotate-key", false, "rotate the agent key")
	switch args[0] {
	case "register":
		if len(args) < 2 {
			usage()
		}
		file := args[1]
		fs.Parse(args[2:])
		raw, err := os.ReadFile(file)
		if err != nil {
			fatal(err)
		}
		u := join(*gw, "/v1/agents")
		if *rotate {
			u += "?rotate_key=true"
		}
		req, _ := http.NewRequest("POST", u, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/yaml")
		if *apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+*apiKey)
		}
		resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
		if err != nil {
			fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		fmt.Println(pretty(b))
		if resp.StatusCode >= 300 {
			os.Exit(1)
		}
	case "list":
		fs.Parse(args[1:])
		code, b, err := doJSON("GET", join(*gw, "/v1/agents"), *apiKey, nil, nil)
		if err != nil {
			fatal(err)
		}
		fmt.Println(pretty(b))
		if code >= 300 {
			os.Exit(1)
		}
	default:
		usage()
	}
}

func auditCmd(args []string) {
	if len(args) == 0 || args[0] != "verify" {
		usage()
	}
	fs := flag.NewFlagSet("audit verify", flag.ExitOnError)
	gw := fs.String("gateway", "", "gateway base URL (remote verification)")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "api key")
	cfgPath := fs.String("config", "", "config file (local verification)")
	data := fs.String("data", "", "data directory (local verification)")
	fs.Parse(args[1:])
	if *gw != "" {
		code, b, err := doJSON("GET", join(*gw, "/v1/audit/verify"), *apiKey, nil, nil)
		if err != nil {
			fatal(err)
		}
		fmt.Println(pretty(b))
		if code != 200 {
			os.Exit(1)
		}
		return
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}
	if *data != "" {
		cfg.DataDir = *data
	}
	var st store.Store
	if cfg.Store.Kind == "postgres" {
		st, err = store.NewPostgres(context.Background(), cfg.Store.DSN)
	} else {
		st, err = store.NewMemStore(filepath.Join(cfg.DataDir, "state"), cfg.Store.MemoryEvents)
	}
	if err != nil {
		fatal(err)
	}
	defer st.Close()
	res := events.Verify(context.Background(), st, true)
	b, _ := json.MarshalIndent(res, "", "  ")
	fmt.Println(string(b))
	if !res.OK {
		os.Exit(1)
	}
}

func mcpStdioCmd(args []string) {
	fs := flag.NewFlagSet("mcp-stdio", flag.ExitOnError)
	gw := fs.String("gateway", "http://localhost:8080", "gateway base URL")
	srv := fs.String("server", "", "mcp server name from the agent manifest")
	agent := fs.String("agent", "", "agent id")
	key := fs.String("key", os.Getenv("RUNGATED_AGENT_KEY"), "agent key")
	token := fs.String("token", os.Getenv("RUNGATED_AGENT_TOKEN"), "agent token")
	session := fs.String("session", "", "session id")
	fs.Parse(args)
	if *srv == "" || (*agent == "" && *token == "") || fs.NArg() == 0 {
		fatal(fmt.Errorf("usage: rungated mcp-stdio --gateway URL --server NAME --agent ID [--key K | --token T] -- command args"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := mcpproxy.RunStdio(ctx, mcpproxy.StdioConfig{
		Gateway: *gw, Server: *srv, Agent: *agent, Key: *key, Token: *token, Session: *session,
		Command: fs.Args(), Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
	})
	if err != nil {
		fatal(err)
	}
}

func tier1Cmd(args []string) {
	if len(args) == 0 || args[0] != "bench" {
		usage()
	}
	fs := flag.NewFlagSet("tier1 bench", flag.ExitOnError)
	cls := fs.String("classifier", "", "classifier onnx file")
	ner := fs.String("ner", "", "ner or gliner onnx file")
	tok := fs.String("tokenizer", "", "tokenizer.json for the classifier")
	ntok := fs.String("ner-tokenizer", "", "tokenizer.json for the ner model")
	labels := fs.String("ner-labels", "", "labels.json for the ner model")
	lib := fs.String("lib", os.Getenv("ORT_LIB"), "libonnxruntime path")
	iters := fs.Int("iters", 200, "iterations per measurement")
	pool := fs.Int("pool", 2, "session pool size")
	threads := fs.Int("threads", 1, "intra op threads per session")
	maxLen := fs.Int("max-len", 256, "maximum tokens per window")
	fs.Parse(args[1:])
	cfg := onnx.Config{ClassifierPath: *cls, NERPath: *ner, TokenizerPath: *tok, NERTokenizerPath: *ntok, NERLabelsPath: *labels, LibraryPath: *lib, PoolSize: *pool, Threads: *threads, MaxLen: *maxLen}
	if err := onnx.Bench(context.Background(), cfg, *iters, os.Stdout); err != nil {
		fatal(err)
	}
}
