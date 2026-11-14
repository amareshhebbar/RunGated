package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

type Tier string

const (
	TierFree    Tier = "free"
	TierTrial30 Tier = "trial30"
	TierTrial14 Tier = "trial14"
)

func (t Tier) TrialDays() int {
	switch t {
	case TierTrial30:
		return 30
	case TierTrial14:
		return 14
	}
	return 0
}

func (t Tier) Label() string {
	switch t {
	case TierTrial30:
		return "30 day trial"
	case TierTrial14:
		return "14 day trial"
	}
	return "free forever"
}

type Kind string

const (
	KindEmbed      Kind = "embed"
	KindClassifier Kind = "classifier"
	KindNER        Kind = "ner"
	KindJudge      Kind = "judge"
)

type Model struct {
	ID          string
	Kind        Kind
	Tier        Tier
	Local       bool
	Hosted      bool
	Base        string
	BaseLicense string
	Params      string
	Languages   string
	Summary     string
	Repo        string
}

var Catalog = []Model{
	{ID: "rungated-embed-mini", Kind: KindEmbed, Tier: TierFree, Local: true, Hosted: true, Base: "sentence-transformers/all-MiniLM-L6-v2", BaseLicense: "Apache 2.0", Params: "22M", Languages: "English", Summary: "small embedder for similarity based attack detection", Repo: "AmareshHebbar/rungated-embed-mini"},
	{ID: "rungated-guard-mini", Kind: KindClassifier, Tier: TierFree, Local: true, Hosted: true, Base: "microsoft/deberta-v3-xsmall", BaseLicense: "MIT", Params: "70M", Languages: "English", Summary: "small prompt injection and jailbreak classifier", Repo: "AmareshHebbar/rungated-guard-mini"},
	{ID: "rungated-embed-multi", Kind: KindEmbed, Tier: TierTrial30, Hosted: true, Base: "intfloat/multilingual-e5-small", BaseLicense: "MIT", Params: "118M", Languages: "100 languages", Summary: "multilingual embedder, catches attacks in other languages"},
	{ID: "rungated-guard-small", Kind: KindClassifier, Tier: TierTrial30, Hosted: true, Base: "microsoft/mdeberta-v3-base", BaseLicense: "MIT", Params: "280M", Languages: "multilingual", Summary: "multilingual injection classifier with six attack heads"},
	{ID: "rungated-pii-small", Kind: KindNER, Tier: TierTrial30, Hosted: true, Base: "urchade/gliner_small-v2.1", BaseLicense: "Apache 2.0", Params: "166M", Languages: "English", Summary: "span NER for names, addresses, IDs and other PII"},
	{ID: "rungated-guard-large", Kind: KindClassifier, Tier: TierTrial14, Hosted: true, Base: "answerdotai/ModernBERT-large", BaseLicense: "Apache 2.0", Params: "395M", Languages: "English", Summary: "highest recall injection classifier, long context"},
	{ID: "rungated-pii-multi", Kind: KindNER, Tier: TierTrial14, Hosted: true, Base: "urchade/gliner_multi_pii-v1", BaseLicense: "Apache 2.0", Params: "209M", Languages: "en, fr, de, es, pt, it", Summary: "multilingual PII NER with 40 plus entity types"},
	{ID: "rungated-judge", Kind: KindJudge, Tier: TierTrial14, Hosted: true, Base: "Qwen/Qwen2.5-1.5B-Instruct", BaseLicense: "Apache 2.0", Params: "1.5B", Languages: "multilingual", Summary: "small LLM judge for held actions"},
}

func Find(id string) (Model, bool) {
	for _, m := range Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func IDs() []string {
	out := make([]string, 0, len(Catalog))
	for _, m := range Catalog {
		out = append(out, m.ID)
	}
	sort.Strings(out)
	return out
}

func (m Model) Delivery() string {
	switch {
	case m.Local && m.Hosted:
		return "download or hosted"
	case m.Local:
		return "download"
	}
	return "hosted"
}

func TrialLeft(t Tier, first, now time.Time) (int, bool) {
	days := t.TrialDays()
	if days == 0 {
		return -1, true
	}
	end := first.Add(time.Duration(days) * 24 * time.Hour)
	if !now.Before(end) {
		return 0, false
	}
	left := int(end.Sub(now).Hours()/24) + 1
	if left > days {
		left = days
	}
	return left, true
}

func Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-22s %-11s %-14s %-19s %s\n", "MODEL", "KIND", "ACCESS", "DELIVERY", "WHAT IT DOES")
	for _, m := range Catalog {
		fmt.Fprintf(&b, "%-22s %-11s %-14s %-19s %s\n", m.ID, m.Kind, m.Tier.Label(), m.Delivery(), m.Summary)
	}
	return b.String()
}
