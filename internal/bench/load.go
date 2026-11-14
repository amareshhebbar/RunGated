package bench

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type LoadLevel struct {
	Concurrency int     `json:"concurrency"`
	Requests    int     `json:"requests"`
	Errors      int     `json:"errors"`
	ErrorRate   float64 `json:"error_rate"`
	Seconds     float64 `json:"seconds"`
	Throughput  float64 `json:"throughput_rps"`
	Client      Latency `json:"client_latency"`
	Server      Latency `json:"server_latency"`
}

type LoadReport struct {
	Version  string      `json:"version"`
	Tier1    bool        `json:"tier1"`
	Setup    LoadSetup   `json:"setup"`
	Levels   []LoadLevel `json:"levels"`
	Started  time.Time   `json:"started"`
	Duration string      `json:"duration_per_level"`
}

type LoadSetup struct {
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUs      int    `json:"cpus"`
	Gateway   string `json:"gateway"`
	Cases     int    `json:"corpus_cases"`
	Mode      string `json:"mode"`
}

func runLevel(ctx context.Context, c *Client, cases []Case, conc int, dur time.Duration) LoadLevel {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: conc * 2, MaxIdleConns: conc * 2}}
	}
	var next atomic.Int64
	var mu sync.Mutex
	var clientUS, serverUS []int64
	errs := 0
	deadline := time.Now().Add(dur)
	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < conc; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cu, su []int64
			e := 0
			for time.Now().Before(deadline) && ctx.Err() == nil {
				i := int(next.Add(1)-1) % len(cases)
				r := c.decide(ctx, cases[i])
				if r.Err != "" {
					e++
					continue
				}
				cu = append(cu, r.ClientUS)
				su = append(su, r.ServerUS)
			}
			mu.Lock()
			clientUS = append(clientUS, cu...)
			serverUS = append(serverUS, su...)
			errs += e
			mu.Unlock()
		}()
	}
	wg.Wait()
	secs := time.Since(start).Seconds()
	total := len(clientUS) + errs
	l := LoadLevel{Concurrency: conc, Requests: total, Errors: errs, Seconds: secs, Client: latency(clientUS), Server: latency(serverUS)}
	if total > 0 {
		l.ErrorRate = float64(errs) / float64(total)
	}
	if secs > 0 {
		l.Throughput = float64(total) / secs
	}
	return l
}

func (r *LoadReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Load report\n\nVersion %s, Tier 1 %v, started %s\n\n", r.Version, r.Tier1, r.Started.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "## Setup\n\n| Item | Value |\n| --- | --- |\n| Go | %s |\n| OS and arch | %s %s |\n| CPUs | %d |\n| Gateway | %s |\n| Mode | %s |\n| Corpus cases | %d |\n| Duration per level | %s |\n\n", r.Setup.GoVersion, r.Setup.OS, r.Setup.Arch, r.Setup.CPUs, r.Setup.Gateway, r.Setup.Mode, r.Setup.Cases, r.Duration)
	b.WriteString("## Results\n\n| Concurrency | Requests | Errors | Error rate | Throughput rps | Client p50 ms | Client p95 ms | Client p99 ms | Decision p50 ms | Decision p95 ms | Decision p99 ms |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, l := range r.Levels {
		fmt.Fprintf(&b, "| %d | %d | %d | %.3f%% | %.0f | %.2f | %.2f | %.2f | %.3f | %.3f | %.3f |\n", l.Concurrency, l.Requests, l.Errors, l.ErrorRate*100, l.Throughput, l.Client.P50, l.Client.P95, l.Client.P99, l.Server.P50, l.Server.P95, l.Server.P99)
	}
	b.WriteString("\nClient latency is the full HTTP round trip seen by the load generator. Decision latency is the time the pipeline reports for the decision itself.\n")
	return b.String()
}

func loadCmd(args []string, version string, stdout io.Writer) error {
	fs := flag.NewFlagSet("bench load", flag.ExitOnError)
	corpus := fs.String("corpus", "", "corpus jsonl, defaults to a generated corpus")
	seed := fs.Int64("seed", 1, "generator seed when no corpus file is given")
	gateway := fs.String("gateway", "", "base URL of a running gateway, otherwise an in process gateway is used")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "api key for a remote gateway")
	agent := fs.String("agent", AgentID, "agent id")
	cfgPath := fs.String("config", "", "gateway config for the in process gateway")
	levels := fs.String("concurrency", "1,8,32,64", "comma separated concurrency levels")
	dur := fs.Duration("duration", 15*time.Second, "duration of each level")
	warm := fs.Duration("warmup", 3*time.Second, "warmup before measuring")
	report := fs.String("report", "", "write the JSON report here")
	md := fs.String("md", "", "write the Markdown report here")
	maxErr := fs.Float64("max-error-rate", 0, "fail when any level exceeds this error rate, 0 disables")
	maxP95 := fs.Float64("max-client-p95-ms", 0, "fail when any level exceeds this client p95, 0 disables")
	fs.Parse(args)
	var cases []Case
	var err error
	if *corpus != "" {
		cases, err = ReadCorpus(*corpus)
		if err != nil {
			return err
		}
	} else {
		cases = Generate(*seed)
	}
	if len(cases) == 0 {
		return errors.New("empty corpus")
	}
	var concs []int
	for _, p := range strings.Split(*levels, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 1 {
			return fmt.Errorf("bad concurrency %q", p)
		}
		concs = append(concs, n)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url, tier1, mode := *gateway, false, "remote gateway"
	if url == "" {
		sh, err := StartSelfHosted(ctx, *cfgPath, version)
		if err != nil {
			return err
		}
		defer sh.Close()
		url, tier1, mode = sh.URL, sh.Tier1, "in process gateway, same host as the load generator"
	} else if *agent == AgentID {
		return errors.New("with --gateway, register internal/bench/agent.yaml on that gateway or pass --agent for an existing agent")
	}
	cl := &Client{BaseURL: url, APIKey: *apiKey, Agent: *agent}
	if *warm > 0 {
		runLevel(ctx, cl, cases, concs[0], *warm)
	}
	rep := &LoadReport{Version: version, Tier1: tier1, Started: time.Now(), Duration: dur.String(), Setup: LoadSetup{GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Gateway: url, Cases: len(cases), Mode: mode}}
	for _, c := range concs {
		if ctx.Err() != nil {
			break
		}
		rep.Levels = append(rep.Levels, runLevel(ctx, cl, cases, c, *dur))
	}
	fmt.Fprint(stdout, rep.Markdown())
	if *report != "" {
		raw, _ := json.MarshalIndent(rep, "", "  ")
		if err := os.WriteFile(*report, raw, 0o644); err != nil {
			return err
		}
	}
	if *md != "" {
		if err := os.WriteFile(*md, []byte(rep.Markdown()), 0o644); err != nil {
			return err
		}
	}
	var fails []string
	for _, l := range rep.Levels {
		if *maxErr > 0 && l.ErrorRate > *maxErr {
			fails = append(fails, fmt.Sprintf("concurrency %d error rate %.4f above %.4f", l.Concurrency, l.ErrorRate, *maxErr))
		}
		if *maxP95 > 0 && l.Client.P95 > *maxP95 {
			fails = append(fails, fmt.Sprintf("concurrency %d client p95 %.2f ms above %.2f ms", l.Concurrency, l.Client.P95, *maxP95))
		}
	}
	if len(fails) > 0 {
		for _, f := range fails {
			fmt.Fprintln(os.Stderr, "LOAD FAIL:", f)
		}
		return fmt.Errorf("load check failed with %d violations", len(fails))
	}
	return nil
}
