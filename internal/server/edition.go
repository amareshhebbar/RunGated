package server

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/amareshhebbar/rungated/internal/config"
	"github.com/amareshhebbar/rungated/internal/contain"
	"github.com/amareshhebbar/rungated/internal/detect"
	"github.com/amareshhebbar/rungated/internal/events"
)

type Edition struct {
	Name      string
	Bus       func(ctx context.Context, cfg *config.Config) (events.Bus, error)
	Sinks     func(ctx context.Context, cfg *config.Config) []events.Sink
	Judge     func(cfg *config.Config) detect.Judge
	Snapshots func(cfg *config.Config) contain.SnapshotStore
	Notifiers func(a *App)
	RBAC      bool
}

var edition = Edition{Name: "free"}

func EditionName() string { return edition.Name }

const enterpriseContact = "hebbar.gvamaresh@gmail.com"

var warned sync.Map

func needsEnterprise(feature string) {
	if _, dup := warned.LoadOrStore(feature, true); dup {
		return
	}
	slog.Warn(feature+" needs Rungated Enterprise and is turned off in this build", "contact", enterpriseContact)
}

func checkKeys(cfg *config.Config) error {
	if edition.RBAC {
		return nil
	}
	for _, k := range cfg.Auth.APIKeys {
		if k.Role != "admin" || k.Tenant != "" {
			return fmt.Errorf("api key %q: roles other than admin and tenant scoped keys need Rungated Enterprise, contact %s", k.Name, enterpriseContact)
		}
	}
	return nil
}

func enterpriseRequested(cfg *config.Config) []string {
	var out []string
	if cfg.Bus.Kind == "nats" || cfg.Bus.Kind == "nats_embedded" {
		out = append(out, "the NATS event bus")
	}
	if cfg.ClickHouse.URL != "" {
		out = append(out, "the ClickHouse sink")
	}
	if cfg.Judge.BaseURL != "" && cfg.Judge.Model != "" {
		out = append(out, "the LLM judge")
	}
	if cfg.Snapshots.Kind == "s3" {
		out = append(out, "S3 forensic snapshots")
	}
	if cfg.Slack.WebhookURL != "" || cfg.Slack.BotToken != "" || cfg.Slack.SigningSecret != "" {
		out = append(out, "Slack approvals")
	}
	if cfg.Email.Addr != "" && len(cfg.Email.To) > 0 {
		out = append(out, "email approvals")
	}
	return out
}
