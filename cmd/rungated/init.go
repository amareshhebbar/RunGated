package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/learn"
	"github.com/amareshhebbar/rungated/pkg/manifest"
	"gopkg.in/yaml.v3"
)

const starterManifest = `apiVersion: rungated.dev/v0
kind: Agent
metadata:
  id: %s
  name: %s
  owner: %s
  purpose: %s
  tenant: %s
  version: "0.1.0"
spec:
  model:
    providers: [openai, anthropic]
    allowed: ["gpt-4o*", "gpt-4.1*", "claude-*"]
  riskTier: medium
  autonomy:
    default: %s
  dataClasses:
    allowed: [public, internal]
    neverEgress: [secret.*, pci.*, pii.ssn]
  tools:
    - name: lookup_order
      server: orders
      description: Fetch one order by id
      blastRadius: low
      args:
        order_id: {type: string, required: true, pattern: "^ORD-[0-9]{5}$"}
    - name: issue_refund
      server: orders
      description: Refund up to 100 dollars on an order
      blastRadius: high
      args:
        order_id: {type: string, required: true, pattern: "^ORD-[0-9]{5}$"}
        amount: {type: number, required: true, min: 1, max: 100}
  egress:
    allow: []
  risk:
    failMode: closed
`

func initCmd(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	id := fs.String("id", "", "agent id (lowercase letters, digits, . _ -)")
	name := fs.String("name", "", "display name")
	owner := fs.String("owner", "", "owner email")
	purpose := fs.String("purpose", "", "one line on what the agent does")
	tenant := fs.String("tenant", "", "tenant id")
	mode := fs.String("mode", "observe", "autonomy mode: observe, recommend, human, auto_contain, full_auto")
	out := fs.String("out", "rungated.agent.yaml", "output file, - for stdout")
	force := fs.Bool("force", false, "overwrite the output file")
	gw := fs.String("from-gateway", "", "learn tools, models and hosts from traffic on this gateway URL")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "admin API key for --from-gateway")
	agent := fs.String("agent", "", "agent id whose traffic to learn from (defaults to --id)")
	limit := fs.Int("limit", 5000, "max events to read")
	evFile := fs.String("from-events", "", "learn from an events.jsonl file instead of a gateway")
	fs.Parse(args)

	if *agent == "" {
		*agent = *id
	}
	if *id == "" {
		*id = *agent
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "usage: rungated init --id my-agent [--owner you@example.com] [--purpose \"...\"] [--from-gateway URL --api-key K | --from-events FILE] [--out FILE]")
		os.Exit(2)
	}
	if !in(manifest.Modes, *mode) {
		fatal(fmt.Errorf("--mode must be one of %v", manifest.Modes))
	}
	if *tenant == "" {
		*tenant = "default"
	}
	if *name == "" {
		*name = titleFromID(*id)
	}
	if *owner == "" {
		*owner = "you@example.com"
	}
	if *purpose == "" {
		*purpose = "Describe what this agent does"
	}

	var data []byte
	var rep *learn.Report
	switch {
	case *gw != "" || *evFile != "":
		var evs []core.Event
		var err error
		if *evFile != "" {
			evs, err = eventsFromFile(*evFile, *agent)
		} else {
			evs, err = eventsFromGateway(*gw, *apiKey, *agent, *limit)
		}
		if err != nil {
			fatal(err)
		}
		m, r := learn.Suggest(evs, learn.Options{AgentID: *agent, Owner: *owner, Purpose: *purpose, Name: *name, Tenant: tenantFlag(fs, *tenant), Mode: *mode})
		m.Metadata.ID = *id
		if m.Metadata.Tenant == "" {
			m.Metadata.Tenant = *tenant
		}
		rep = &r
		data, err = yaml2(m)
		if err != nil {
			fatal(err)
		}
	default:
		data = []byte(fmt.Sprintf(starterManifest, *id, yamlQuote(*name), yamlQuote(*owner), yamlQuote(*purpose), *tenant, *mode))
	}

	if _, err := manifest.Parse(data); err != nil {
		fatal(fmt.Errorf("generated manifest does not validate: %w", err))
	}

	if *out == "-" {
		os.Stdout.Write(data)
	} else {
		if _, err := os.Stat(*out); err == nil && !*force {
			fatal(fmt.Errorf("%s exists, use --force to overwrite", *out))
		}
		if err := os.WriteFile(*out, data, 0o644); err != nil {
			fatal(err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *out)
	}
	if rep != nil {
		fmt.Fprintf(os.Stderr, "read %d events, used %d, skipped %d flagged as attacks or secrets\n", rep.Events, rep.Used, rep.SkippedRisk)
		fmt.Fprintf(os.Stderr, "found %d tools, %d models, %d egress hosts\n", rep.Tools, rep.Models, rep.Hosts)
		for _, n := range rep.Notes {
			fmt.Fprintln(os.Stderr, "note:", n)
		}
		fmt.Fprintln(os.Stderr, "review the limits before switching the agent to auto_contain")
	} else {
		fmt.Fprintln(os.Stderr, "edit the tools list to match your agent, then:")
	}
	if *out != "-" {
		fmt.Fprintf(os.Stderr, "  rungated validate %s\n", *out)
		fmt.Fprintf(os.Stderr, "  rungated agents register %s --gateway http://localhost:8080 --api-key $RUNGATED_API_KEY\n", *out)
	}
}

func tenantFlag(fs *flag.FlagSet, v string) string {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "tenant" {
			set = true
		}
	})
	if set {
		return v
	}
	return ""
}

func eventsFromFile(path, agent string) ([]core.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return learn.ReadEvents(f, agent)
}

func eventsFromGateway(base, apiKey, agent string, limit int) ([]core.Event, error) {
	if agent == "" {
		return nil, errors.New("--agent or --id is required with --from-gateway")
	}
	seen := map[string]bool{}
	var all []core.Event
	before := ""
	for len(all) < limit {
		page := limit - len(all)
		if page > 1000 {
			page = 1000
		}
		q := url.Values{"agent": {agent}, "limit": {strconv.Itoa(page)}}
		if before != "" {
			q.Set("before", before)
		}
		code, body, err := doJSON("GET", join(base, "/v1/events?"+q.Encode()), apiKey, nil, nil)
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("gateway returned %d: %s", code, strings.TrimSpace(string(body)))
		}
		var resp struct {
			Events []core.Event `json:"events"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		added := 0
		var oldest time.Time
		for _, e := range resp.Events {
			if oldest.IsZero() || e.Time.Before(oldest) {
				oldest = e.Time
			}
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			all = append(all, e)
			added++
		}
		if added == 0 || len(resp.Events) < page {
			break
		}
		before = oldest.UTC().Format(time.RFC3339Nano)
	}
	return all, nil
}

func yaml2(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	enc.Close()
	return b.Bytes(), nil
}

func titleFromID(id string) string {
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

func yamlQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func in(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
