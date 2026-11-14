//go:build !enterprise

package server

import (
	"context"
	"strings"
	"testing"

	"github.com/amareshhebbar/rungated/internal/config"
)

func TestFreeRejectsRoles(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Auth.APIKeys = []config.APIKeyConfig{{Key: "a", Name: "root", Role: "admin"}, {Key: "b", Name: "viewer", Role: "auditor"}}
	if err := cfg.Finalize(); err != nil {
		t.Fatal(err)
	}
	_, err := New(context.Background(), cfg, "test")
	if err == nil || !strings.Contains(err.Error(), "Enterprise") {
		t.Fatalf("want enterprise error, got %v", err)
	}
}

func TestFreeIgnoresEnterpriseConfig(t *testing.T) {
	e := newEnv(t, func(c *config.Config) {
		c.Bus.Kind = "nats_embedded"
		c.Slack.SigningSecret = "shh"
		c.Judge.BaseURL = "http://127.0.0.1:1"
		c.Judge.Model = "m"
	})
	if e.app.Engine.Judge != nil {
		t.Fatal("judge should be off in the free build")
	}
	code, _, _ := e.do("POST", "/v1/integrations/slack/actions", "payload=x", nil)
	if code != 404 && code != 405 {
		t.Fatalf("slack route should not exist, got %d", code)
	}
	_, _, b := e.do("GET", "/v1/version", nil, nil)
	if !strings.Contains(string(b), `"edition":"free"`) {
		t.Fatalf("version: %s", b)
	}
}
