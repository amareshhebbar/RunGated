package detect

import (
	"regexp"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
)

type secretRule struct {
	ID         string
	Class      string
	Keywords   []string
	Re         *regexp.Regexp
	Group      int
	MinEntropy float64
	Severity   float64
	Confidence float64
}

var secretRules = []secretRule{
	{ID: "aws-access-key-id", Class: "secret.aws.access_key", Keywords: []string{"akia", "asia", "abia", "acca"}, Re: regexp.MustCompile(`\b((?:AKIA|ASIA|ABIA|ACCA)[A-Z2-7]{16})\b`), Group: 1, Severity: 0.9, Confidence: 0.95},
	{ID: "aws-secret-access-key", Class: "secret.aws.secret_key", Keywords: []string{"aws", "secret"}, Re: regexp.MustCompile(`(?i)(?:aws)?_?(?:secret)_?(?:access)?_?key(?:_id)?["'\s]*[:=]\s*["']?([A-Za-z0-9/+=]{40})\b`), Group: 1, MinEntropy: 3.8, Severity: 0.95, Confidence: 0.9},
	{ID: "github-pat", Class: "secret.github.token", Keywords: []string{"ghp_", "gho_", "ghu_", "ghs_", "ghr_"}, Re: regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{36,255})\b`), Group: 1, Severity: 0.9, Confidence: 0.97},
	{ID: "github-fine-grained-pat", Class: "secret.github.token", Keywords: []string{"github_pat_"}, Re: regexp.MustCompile(`\b(github_pat_[A-Za-z0-9_]{60,255})\b`), Group: 1, Severity: 0.9, Confidence: 0.97},
	{ID: "gitlab-pat", Class: "secret.gitlab.token", Keywords: []string{"glpat-"}, Re: regexp.MustCompile(`\b(glpat-[A-Za-z0-9_\-]{20,})\b`), Group: 1, Severity: 0.9, Confidence: 0.95},
	{ID: "slack-token", Class: "secret.slack.token", Keywords: []string{"xox"}, Re: regexp.MustCompile(`\b(xox[baprse]-[A-Za-z0-9-]{10,})\b`), Group: 1, Severity: 0.85, Confidence: 0.95},
	{ID: "slack-webhook", Class: "secret.slack.webhook", Keywords: []string{"hooks.slack.com"}, Re: regexp.MustCompile(`(https://hooks\.slack\.com/(?:services|workflows)/[A-Za-z0-9+/]{20,})`), Group: 1, Severity: 0.8, Confidence: 0.95},
	{ID: "stripe-key", Class: "secret.stripe.key", Keywords: []string{"sk_live", "sk_test", "rk_live", "rk_test"}, Re: regexp.MustCompile(`\b((?:sk|rk)_(?:live|test)_[A-Za-z0-9]{20,99})\b`), Group: 1, Severity: 0.95, Confidence: 0.97},
	{ID: "google-api-key", Class: "secret.gcp.api_key", Keywords: []string{"aiza"}, Re: regexp.MustCompile(`\b(AIza[0-9A-Za-z\-_]{35})\b`), Group: 1, Severity: 0.85, Confidence: 0.95},
	{ID: "gcp-service-account", Class: "secret.gcp.service_account", Keywords: []string{"service_account"}, Re: regexp.MustCompile(`"type"\s*:\s*"service_account"[\s\S]{0,400}?"private_key"\s*:\s*"(-----BEGIN[^"]{40,})"`), Group: 1, Severity: 0.98, Confidence: 0.97},
	{ID: "openai-key", Class: "secret.openai.key", Keywords: []string{"sk-"}, Re: regexp.MustCompile(`\b(sk-(?:proj-|svcacct-|admin-)?[A-Za-z0-9_\-]{20,200}T3BlbkFJ[A-Za-z0-9_\-]{20,200}|sk-proj-[A-Za-z0-9_\-]{40,200})\b`), Group: 1, Severity: 0.9, Confidence: 0.95},
	{ID: "anthropic-key", Class: "secret.anthropic.key", Keywords: []string{"sk-ant-"}, Re: regexp.MustCompile(`\b(sk-ant-(?:api|admin)\d{2}-[A-Za-z0-9_\-]{20,200})`), Group: 1, Severity: 0.9, Confidence: 0.97},
	{ID: "huggingface-token", Class: "secret.huggingface.token", Keywords: []string{"hf_"}, Re: regexp.MustCompile(`\b(hf_[A-Za-z]{34,40})\b`), Group: 1, Severity: 0.8, Confidence: 0.9},
	{ID: "private-key", Class: "secret.private_key", Keywords: []string{"private key"}, Re: regexp.MustCompile(`(-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY(?: BLOCK)?-----[\s\S]*?(?:-----END[^-]{0,40}-----|$))`), Group: 1, Severity: 0.98, Confidence: 0.98},
	{ID: "jwt", Class: "secret.jwt", Keywords: []string{"eyj"}, Re: regexp.MustCompile(`\b(eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})`), Group: 1, Severity: 0.7, Confidence: 0.85},
	{ID: "twilio-api-key", Class: "secret.twilio.key", Keywords: []string{"sk"}, Re: regexp.MustCompile(`\b(SK[0-9a-fA-F]{32})\b`), Group: 1, MinEntropy: 3.2, Severity: 0.8, Confidence: 0.75},
	{ID: "sendgrid-key", Class: "secret.sendgrid.key", Keywords: []string{"sg."}, Re: regexp.MustCompile(`\b(SG\.[A-Za-z0-9_\-]{22}\.[A-Za-z0-9_\-]{43})\b`), Group: 1, Severity: 0.85, Confidence: 0.97},
	{ID: "npm-token", Class: "secret.npm.token", Keywords: []string{"npm_"}, Re: regexp.MustCompile(`\b(npm_[A-Za-z0-9]{36})\b`), Group: 1, Severity: 0.85, Confidence: 0.95},
	{ID: "pypi-token", Class: "secret.pypi.token", Keywords: []string{"pypi-"}, Re: regexp.MustCompile(`\b(pypi-AgEIcHlwaS5vcmc[A-Za-z0-9_\-]{50,})`), Group: 1, Severity: 0.85, Confidence: 0.97},
	{ID: "shopify-token", Class: "secret.shopify.token", Keywords: []string{"shpat_", "shpss_", "shpca_", "shppa_"}, Re: regexp.MustCompile(`\b(shp(?:at|ss|ca|pa)_[a-fA-F0-9]{32})\b`), Group: 1, Severity: 0.85, Confidence: 0.97},
	{ID: "databricks-token", Class: "secret.databricks.token", Keywords: []string{"dapi"}, Re: regexp.MustCompile(`\b(dapi[a-h0-9]{32})\b`), Group: 1, Severity: 0.85, Confidence: 0.9},
	{ID: "mailgun-key", Class: "secret.mailgun.key", Keywords: []string{"key-"}, Re: regexp.MustCompile(`\b(key-[0-9a-zA-Z]{32})\b`), Group: 1, MinEntropy: 3.5, Severity: 0.8, Confidence: 0.8},
	{ID: "azure-storage-key", Class: "secret.azure.storage_key", Keywords: []string{"accountkey="}, Re: regexp.MustCompile(`(?i)AccountKey=([A-Za-z0-9+/=]{86,88})`), Group: 1, Severity: 0.95, Confidence: 0.97},
	{ID: "connection-string-password", Class: "secret.db.password", Keywords: []string{"://"}, Re: regexp.MustCompile(`(?i)\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|rediss|amqps?|mssql|sqlserver)://[^:\s/@]+:([^@\s/]{3,})@[^\s]+`), Group: 1, Severity: 0.9, Confidence: 0.9},
	{ID: "generic-credential", Class: "secret.generic", Keywords: []string{"key", "secret", "token", "pass", "pwd", "credential"}, Re: regexp.MustCompile(`(?i)\b(?:api[_-]?key|apikey|secret(?:[_-]?key)?|access[_-]?token|auth[_-]?token|token|passwd|password|pwd|client[_-]?secret|private[_-]?key)\b["']?\s*[:=]\s*["']?([A-Za-z0-9_\-/+=.!@#$%^&*~]{10,200})`), Group: 1, MinEntropy: 3.3, Severity: 0.75, Confidence: 0.7},
}

var placeholderRe = regexp.MustCompile(`(?i)^(?:x+|\*+|\.+|changeme|change_me|password|secret|example|redacted|null|none|undefined|your[_-].*|<.*>|\$\{.*\}|\{\{.*\}\}|test|dummy|sample)$|\[\[(?:PII|SECRET|PCI|PHI)_`)

type SecretsDetector struct{}

func (SecretsDetector) Name() string { return "secrets" }

func (SecretsDetector) Scan(part int, text string) []core.Finding {
	if len(text) < 8 {
		return nil
	}
	lower := strings.ToLower(text)
	var out []core.Finding
	taken := map[[2]int]bool{}
	for _, r := range secretRules {
		hit := false
		for _, k := range r.Keywords {
			if strings.Contains(lower, k) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, m := range r.Re.FindAllStringSubmatchIndex(text, 32) {
			s, e := m[2*r.Group], m[2*r.Group+1]
			if s < 0 {
				continue
			}
			val := text[s:e]
			if placeholderRe.MatchString(val) {
				continue
			}
			if r.MinEntropy > 0 && Entropy(val) < r.MinEntropy {
				continue
			}
			overlap := false
			for k := range taken {
				if s < k[1] && e > k[0] {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			taken[[2]int{s, e}] = true
			out = append(out, core.Finding{
				Detector:   "secrets",
				Tier:       0,
				Category:   core.CatSecret,
				Rule:       r.ID,
				Severity:   r.Severity,
				Confidence: r.Confidence,
				Message:    "credential detected: " + r.ID,
				DataClass:  r.Class,
				Span:       &core.Span{Part: part, Start: s, End: e},
				Redactable: true,
				Match:      val,
			})
		}
	}
	return out
}
