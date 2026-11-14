package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
)

func WriteCorpus(path string, cases []Case) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, c := range cases {
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		w.Write(raw)
		w.WriteByte('\n')
	}
	return w.Flush()
}

func ReadCorpus(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var c Case
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

func Main(args []string, version string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: rungated bench <gen|run|load> [flags]")
	}
	switch args[0] {
	case "gen":
		fs := flag.NewFlagSet("bench gen", flag.ExitOnError)
		out := fs.String("out", "bench/corpus.jsonl", "output file")
		seed := fs.Int64("seed", 1, "generator seed")
		fs.Parse(args[1:])
		cases := Generate(*seed)
		if err := WriteCorpus(*out, cases); err != nil {
			return err
		}
		counts := map[string]int{}
		for _, c := range cases {
			counts[c.Class]++
		}
		keys := make([]string, 0, len(counts))
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(stdout, "wrote %d cases to %s\n", len(cases), *out)
		for _, k := range keys {
			fmt.Fprintf(stdout, "  %-24s %d\n", k, counts[k])
		}
		return nil
	case "run":
		return runCmd(args[1:], version, stdout)
	case "load":
		return loadCmd(args[1:], version, stdout)
	}
	return fmt.Errorf("unknown bench command %q", args[0])
}

func runCmd(args []string, version string, stdout io.Writer) error {
	fs := flag.NewFlagSet("bench run", flag.ExitOnError)
	corpus := fs.String("corpus", "", "corpus jsonl, defaults to a freshly generated corpus with seed 1")
	seed := fs.Int64("seed", 1, "generator seed when no corpus file is given")
	gateway := fs.String("gateway", "", "base URL of a running gateway, otherwise an in process gateway is used")
	apiKey := fs.String("api-key", os.Getenv("RUNGATED_API_KEY"), "api key for a remote gateway")
	agent := fs.String("agent", AgentID, "agent id that the corpus is evaluated as")
	cfgPath := fs.String("config", "", "gateway config for the in process gateway, for example to enable Tier 1")
	conc := fs.Int("concurrency", 8, "parallel requests")
	report := fs.String("report", "", "write the JSON summary here")
	md := fs.String("md", "", "write the Markdown report here")
	gate := fs.Bool("gate", false, "fail when the run regresses against the baseline")
	baselinePath := fs.String("baseline", "bench/baseline.json", "baseline file")
	update := fs.Bool("update-baseline", false, "write the current results as the new baseline")
	maxP95 := fs.Float64("max-p95-ms", 40, "decision latency p95 limit stored when updating the baseline")
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	url, tier1 := *gateway, false
	if url == "" {
		sh, err := StartSelfHosted(ctx, *cfgPath, version)
		if err != nil {
			return err
		}
		defer sh.Close()
		url, tier1 = sh.URL, sh.Tier1
	} else if *agent == AgentID {
		return errors.New("with --gateway, register bench/agent.yaml on that gateway (internal/bench/agent.yaml) or pass --agent for an existing agent")
	}
	cl := &Client{BaseURL: url, APIKey: *apiKey, Agent: *agent}
	results := Run(ctx, cl, cases, *conc)
	s := Summarize(results, version, tier1)
	fmt.Fprint(stdout, s.Markdown())
	if *report != "" {
		raw, _ := json.MarshalIndent(s, "", "  ")
		if err := os.WriteFile(*report, raw, 0o644); err != nil {
			return err
		}
	}
	if *md != "" {
		if err := os.WriteFile(*md, []byte(s.Markdown()), 0o644); err != nil {
			return err
		}
	}
	if *update {
		if err := SaveBaseline(*baselinePath, BaselineFrom(s, *maxP95)); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "baseline written to %s\n", *baselinePath)
	}
	if *gate {
		b, err := LoadBaseline(*baselinePath)
		if err != nil {
			return err
		}
		if fails := b.Check(s); len(fails) > 0 {
			for _, f := range fails {
				fmt.Fprintln(os.Stderr, "GATE FAIL:", f)
			}
			return fmt.Errorf("bench gate failed with %d regressions", len(fails))
		}
		fmt.Fprintln(stdout, "bench gate passed")
	}
	return nil
}
