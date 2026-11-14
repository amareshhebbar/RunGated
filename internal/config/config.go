package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type StoreConfig struct {
	Kind         string `yaml:"kind"`
	DSN          string `yaml:"dsn"`
	MemoryEvents int    `yaml:"memory_events"`
}

type BusConfig struct {
	Kind string `yaml:"kind"`
	URL  string `yaml:"url"`
	Port int    `yaml:"port"`
}

type ClickHouseConfig struct {
	URL      string `yaml:"url"`
	Database string `yaml:"database"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

type WebhookConfig struct {
	URL        string `yaml:"url"`
	Secret     string `yaml:"secret"`
	Format     string `yaml:"format"`
	Token      string `yaml:"token"`
	MinVerdict string `yaml:"min_verdict"`
}

type Upstream struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type Upstreams struct {
	OpenAI    Upstream            `yaml:"openai"`
	Anthropic Upstream            `yaml:"anthropic"`
	Extra     map[string]Upstream `yaml:"extra"`
}

type APIKeyConfig struct {
	Key    string `yaml:"key"`
	Name   string `yaml:"name"`
	Role   string `yaml:"role"`
	Tenant string `yaml:"tenant"`
}

type AuthConfig struct {
	RequireAgentAuth   bool           `yaml:"require_agent_auth"`
	APIKeys            []APIKeyConfig `yaml:"api_keys"`
	DisableConsoleAuth bool           `yaml:"disable_console_auth"`
}

type JudgeConfig struct {
	BaseURL   string `yaml:"base_url"`
	APIKey    string `yaml:"api_key"`
	Model     string `yaml:"model"`
	TimeoutMS int    `yaml:"timeout_ms"`
	Style     string `yaml:"style"`
}

type SlackConfig struct {
	WebhookURL    string   `yaml:"webhook_url"`
	BotToken      string   `yaml:"bot_token"`
	Channel       string   `yaml:"channel"`
	SigningSecret string   `yaml:"signing_secret"`
	AllowedUsers  []string `yaml:"allowed_users"`
}

type EmailConfig struct {
	Addr     string   `yaml:"addr"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
}

type SnapshotConfig struct {
	Kind      string `yaml:"kind"`
	Dir       string `yaml:"dir"`
	Endpoint  string `yaml:"endpoint"`
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	PathStyle bool   `yaml:"path_style"`
}

type Price struct {
	InputPer1M  float64 `yaml:"input_per_1m"`
	OutputPer1M float64 `yaml:"output_per_1m"`
}

type Tier1Config struct {
	Enabled        bool   `yaml:"enabled"`
	Model          string `yaml:"model"`
	ClassifierPath string `yaml:"classifier_path"`
	NERPath        string `yaml:"ner_path"`
	TokenizerPath  string `yaml:"tokenizer_path"`
	LibraryPath    string `yaml:"library_path"`

	NERLabelsPath    string  `yaml:"ner_labels_path"`
	NERTokenizerPath string  `yaml:"ner_tokenizer_path"`
	NERThreshold     float64 `yaml:"ner_threshold"`
	InjectionIndex   *int    `yaml:"injection_index"`
	Threshold        float64 `yaml:"threshold"`
	PoolSize         int     `yaml:"pool_size"`
	Threads          int     `yaml:"threads"`
	MaxLen           int     `yaml:"max_len"`
	Stride           int     `yaml:"stride"`
	MaxWindows       int     `yaml:"max_windows"`
	TimeoutMS        int     `yaml:"timeout_ms"`
	Temperature      float64 `yaml:"temperature"`
}

type EmbedConfig struct {
	Enabled       bool    `yaml:"enabled"`
	Model         string  `yaml:"model"`
	ModelPath     string  `yaml:"model_path"`
	TokenizerPath string  `yaml:"tokenizer_path"`
	BankPath      string  `yaml:"bank_path"`
	Threshold     float64 `yaml:"threshold"`
	MaxLen        int     `yaml:"max_len"`
}

type CloudConfig struct {
	URL        string `yaml:"url"`
	APIKey     string `yaml:"api_key"`
	APIKeyEnv  string `yaml:"api_key_env"`
	Classifier string `yaml:"classifier"`
	NER        string `yaml:"ner"`
	Embedder   string `yaml:"embedder"`
	TimeoutMS  int    `yaml:"timeout_ms"`
}

func (c CloudConfig) Enabled() bool {
	return c.APIKey != "" && (c.Classifier != "" || c.NER != "" || c.Embedder != "")
}

type EgressConfig struct {
	InterceptTLS bool   `yaml:"intercept_tls"`
	MaxScanKB    int    `yaml:"max_scan_kb"`
	ParentProxy  string `yaml:"parent_proxy"`
}

type Config struct {
	Listen        string            `yaml:"listen"`
	EgressListen  string            `yaml:"egress_listen"`
	Egress        EgressConfig      `yaml:"egress"`
	DataDir       string            `yaml:"data_dir"`
	Store         StoreConfig       `yaml:"store"`
	Bus           BusConfig         `yaml:"bus"`
	ClickHouse    ClickHouseConfig  `yaml:"clickhouse"`
	Webhooks      []WebhookConfig   `yaml:"webhooks"`
	Upstreams     Upstreams         `yaml:"upstreams"`
	Auth          AuthConfig        `yaml:"auth"`
	UnknownAgents string            `yaml:"unknown_agents"`
	Judge         JudgeConfig       `yaml:"judge"`
	Slack         SlackConfig       `yaml:"slack"`
	Email         EmailConfig       `yaml:"email"`
	ConsoleURL    string            `yaml:"console_url"`
	Snapshots     SnapshotConfig    `yaml:"snapshots"`
	MasterKeyFile string            `yaml:"master_key_file"`
	TokenTTLSecs  int               `yaml:"token_ttl_seconds"`
	Prices        map[string]Price  `yaml:"prices"`
	Manifests     []string          `yaml:"manifests"`
	Tier1         Tier1Config       `yaml:"tier1"`
	Embed         EmbedConfig       `yaml:"embed"`
	Cloud         CloudConfig       `yaml:"cloud"`
	CORSOrigins   []string          `yaml:"cors_origins"`
	MaxBodyMB     int               `yaml:"max_body_mb"`
	Dev           bool              `yaml:"dev"`
	Labels        map[string]string `yaml:"labels"`
}

func DefaultPrices() map[string]Price {
	return map[string]Price{
		"gpt-4o-mini":       {0.15, 0.60},
		"gpt-4o":            {2.50, 10.00},
		"gpt-4.1-nano":      {0.10, 0.40},
		"gpt-4.1-mini":      {0.40, 1.60},
		"gpt-4.1":           {2.00, 8.00},
		"gpt-5-mini":        {0.25, 2.00},
		"gpt-5":             {1.25, 10.00},
		"o4-mini":           {1.10, 4.40},
		"o3":                {2.00, 8.00},
		"claude-haiku-4":    {1.00, 5.00},
		"claude-sonnet-4":   {3.00, 15.00},
		"claude-sonnet-5":   {3.00, 15.00},
		"claude-opus-4":     {15.00, 75.00},
		"claude-opus-5":     {5.00, 25.00},
		"claude-3-5-haiku":  {0.80, 4.00},
		"claude-3-5-sonnet": {3.00, 15.00},
		"mock-":             {0.50, 1.50},
	}
}

func Default() *Config {
	return &Config{
		Listen:        ":8080",
		EgressListen:  ":8081",
		DataDir:       "./data",
		Store:         StoreConfig{Kind: "embedded"},
		Bus:           BusConfig{Kind: "local", Port: 4223},
		ClickHouse:    ClickHouseConfig{Database: "rungated", User: "default"},
		Upstreams:     Upstreams{OpenAI: Upstream{BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"}, Anthropic: Upstream{BaseURL: "https://api.anthropic.com/v1", APIKeyEnv: "ANTHROPIC_API_KEY"}},
		UnknownAgents: "reject",
		Judge:         JudgeConfig{TimeoutMS: 2000, Style: "chat"},
		Snapshots:     SnapshotConfig{Kind: "file"},
		TokenTTLSecs:  900,
		Prices:        DefaultPrices(),
		MaxBodyMB:     20,
		Cloud:         CloudConfig{URL: "https://api.rungated.dev", APIKeyEnv: "RUNGATED_CLOUD_API_KEY", TimeoutMS: 400},
	}
}

func Load(path string) (*Config, error) {
	c := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(c); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if len(c.Prices) == 0 {
			c.Prices = DefaultPrices()
		} else {
			merged := DefaultPrices()
			for k, v := range c.Prices {
				merged[k] = v
			}
			c.Prices = merged
		}
	}
	c.ApplyEnv()
	return c, c.Finalize()
}

func env(k string) string { return strings.TrimSpace(os.Getenv(k)) }

func (c *Config) ApplyEnv() {
	set := func(dst *string, key string) {
		if v := env(key); v != "" {
			*dst = v
		}
	}
	set(&c.Listen, "RUNGATED_LISTEN")
	set(&c.EgressListen, "RUNGATED_EGRESS_LISTEN")
	set(&c.DataDir, "RUNGATED_DATA_DIR")
	set(&c.Store.DSN, "RUNGATED_DATABASE_URL")
	if c.Store.DSN != "" && env("RUNGATED_STORE") == "" && c.Store.Kind == "embedded" {
		c.Store.Kind = "postgres"
	}
	set(&c.Store.Kind, "RUNGATED_STORE")
	if v := env("RUNGATED_MEMORY_EVENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Store.MemoryEvents = n
		}
	}
	set(&c.Bus.Kind, "RUNGATED_BUS")
	set(&c.Bus.URL, "RUNGATED_NATS_URL")
	set(&c.ClickHouse.URL, "RUNGATED_CLICKHOUSE_URL")
	set(&c.ClickHouse.Database, "RUNGATED_CLICKHOUSE_DB")
	set(&c.ClickHouse.User, "RUNGATED_CLICKHOUSE_USER")
	set(&c.ClickHouse.Password, "RUNGATED_CLICKHOUSE_PASSWORD")
	set(&c.Upstreams.OpenAI.BaseURL, "RUNGATED_OPENAI_BASE_URL")
	set(&c.Upstreams.Anthropic.BaseURL, "RUNGATED_ANTHROPIC_BASE_URL")
	set(&c.Judge.BaseURL, "RUNGATED_JUDGE_BASE_URL")
	set(&c.Judge.APIKey, "RUNGATED_JUDGE_API_KEY")
	set(&c.Judge.Model, "RUNGATED_JUDGE_MODEL")
	set(&c.Slack.WebhookURL, "RUNGATED_SLACK_WEBHOOK_URL")
	set(&c.Slack.BotToken, "RUNGATED_SLACK_BOT_TOKEN")
	set(&c.Slack.Channel, "RUNGATED_SLACK_CHANNEL")
	set(&c.Slack.SigningSecret, "RUNGATED_SLACK_SIGNING_SECRET")
	if v := env("RUNGATED_SLACK_ALLOWED_USERS"); v != "" {
		c.Slack.AllowedUsers = nil
		for _, u := range strings.Split(v, ",") {
			if u = strings.TrimSpace(u); u != "" {
				c.Slack.AllowedUsers = append(c.Slack.AllowedUsers, u)
			}
		}
	}
	set(&c.ConsoleURL, "RUNGATED_CONSOLE_URL")
	set(&c.UnknownAgents, "RUNGATED_UNKNOWN_AGENTS")
	set(&c.Tier1.LibraryPath, "ORT_LIB")
	set(&c.Tier1.ClassifierPath, "RUNGATED_TIER1_CLASSIFIER")
	set(&c.Tier1.NERPath, "RUNGATED_TIER1_NER")
	set(&c.Tier1.TokenizerPath, "RUNGATED_TIER1_TOKENIZER")
	if v := env("RUNGATED_TIER1_ENABLED"); v != "" {
		c.Tier1.Enabled, _ = strconv.ParseBool(v)
	}
	if v := env("RUNGATED_API_KEY"); v != "" {
		c.Auth.APIKeys = append(c.Auth.APIKeys, APIKeyConfig{Key: v, Name: "env-admin", Role: "admin"})
	}
	if v := env("RUNGATED_REQUIRE_AGENT_AUTH"); v != "" {
		c.Auth.RequireAgentAuth, _ = strconv.ParseBool(v)
	}
	set(&c.Egress.ParentProxy, "RUNGATED_EGRESS_PARENT_PROXY")
	if v := env("RUNGATED_EGRESS_INTERCEPT_TLS"); v != "" {
		c.Egress.InterceptTLS, _ = strconv.ParseBool(v)
	}
	set(&c.Cloud.URL, "RUNGATED_CLOUD_URL")
	set(&c.Cloud.Classifier, "RUNGATED_CLOUD_CLASSIFIER")
	set(&c.Cloud.NER, "RUNGATED_CLOUD_NER")
	set(&c.Cloud.Embedder, "RUNGATED_CLOUD_EMBEDDER")
	set(&c.Embed.Model, "RUNGATED_EMBED_MODEL")
	if c.Embed.Model != "" && env("RUNGATED_EMBED_MODEL") != "" {
		c.Embed.Enabled = true
	}
	if v := env("RUNGATED_DEV"); v != "" {
		c.Dev, _ = strconv.ParseBool(v)
	}
}

func (c *Config) Finalize() error {
	if c.Cloud.APIKey == "" && c.Cloud.APIKeyEnv != "" {
		c.Cloud.APIKey = env(c.Cloud.APIKeyEnv)
	}
	if c.Cloud.URL == "" {
		c.Cloud.URL = "https://api.rungated.dev"
	}
	if c.Cloud.TimeoutMS <= 0 {
		c.Cloud.TimeoutMS = 400
	}
	if c.Embed.Threshold <= 0 {
		c.Embed.Threshold = 0.72
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	if c.Store.MemoryEvents <= 0 {
		c.Store.MemoryEvents = 20000
	}
	switch c.Store.Kind {
	case "", "embedded":
		c.Store.Kind = "embedded"
	case "postgres":
		if c.Store.DSN == "" {
			return errors.New("store.kind postgres requires store.dsn")
		}
	default:
		return fmt.Errorf("unknown store kind %q", c.Store.Kind)
	}
	switch c.Bus.Kind {
	case "", "local":
		c.Bus.Kind = "local"
	case "nats_embedded":
	case "nats":
		if c.Bus.URL == "" {
			return errors.New("bus.kind nats requires bus.url")
		}
	default:
		return fmt.Errorf("unknown bus kind %q", c.Bus.Kind)
	}
	switch c.UnknownAgents {
	case "", "reject":
		c.UnknownAgents = "reject"
	case "observe":
	default:
		return fmt.Errorf("unknown_agents must be reject or observe")
	}
	for _, u := range []*Upstream{&c.Upstreams.OpenAI, &c.Upstreams.Anthropic} {
		if u.APIKey == "" && u.APIKeyEnv != "" {
			u.APIKey = env(u.APIKeyEnv)
		}
	}
	for k, u := range c.Upstreams.Extra {
		if u.APIKey == "" && u.APIKeyEnv != "" {
			u.APIKey = env(u.APIKeyEnv)
			c.Upstreams.Extra[k] = u
		}
	}
	for _, k := range c.Auth.APIKeys {
		switch k.Role {
		case "admin", "security", "developer", "auditor":
		default:
			return fmt.Errorf("api key %q has invalid role %q", k.Name, k.Role)
		}
	}
	if c.MaxBodyMB <= 0 {
		c.MaxBodyMB = 20
	}
	if c.TokenTTLSecs <= 0 {
		c.TokenTTLSecs = 900
	}
	if c.MasterKeyFile == "" {
		c.MasterKeyFile = filepath.Join(c.DataDir, "master.key")
	}
	if c.Snapshots.Kind == "" {
		c.Snapshots.Kind = "file"
	}
	if c.Snapshots.Kind == "file" && c.Snapshots.Dir == "" {
		c.Snapshots.Dir = filepath.Join(c.DataDir, "snapshots")
	}
	return nil
}

func (c *Config) MasterKey() ([]byte, error) {
	if v := env("RUNGATED_MASTER_KEY"); v != "" {
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("RUNGATED_MASTER_KEY is not base64: %w", err)
		}
		if len(b) < 32 {
			return nil, errors.New("RUNGATED_MASTER_KEY must decode to at least 32 bytes")
		}
		return b, nil
	}
	if raw, err := os.ReadFile(c.MasterKeyFile); err == nil {
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(b) < 32 {
			return nil, fmt.Errorf("master key file %s is corrupt", c.MasterKeyFile)
		}
		return b, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(c.MasterKeyFile), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(c.MasterKeyFile, []byte(base64.StdEncoding.EncodeToString(b)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return b, nil
}
