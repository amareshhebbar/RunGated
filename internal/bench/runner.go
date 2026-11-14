package bench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var rank = map[string]int{"allow": 0, "warn": 1, "redact": 2, "hold": 3, "block": 4, "contain": 5}

type Result struct {
	Case      Case     `json:"case"`
	Computed  string   `json:"computed"`
	Score     int      `json:"score"`
	Detectors []string `json:"detectors,omitempty"`
	ServerUS  int64    `json:"server_us"`
	ClientUS  int64    `json:"client_us"`
	Tier1US   int64    `json:"tier1_us,omitempty"`
	Err       string   `json:"error,omitempty"`
}

func (r Result) Caught() bool  { return rank[r.Computed] >= rank["redact"] }
func (r Result) Stopped() bool { return rank[r.Computed] >= rank["hold"] }

type Client struct {
	BaseURL string
	APIKey  string
	Agent   string
	HTTP    *http.Client
}

func (c *Client) decide(ctx context.Context, cs Case) Result {
	res := Result{Case: cs}
	body, _ := json.Marshal(cs.request(c.Agent))
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+"/v1/decide", bytes.NewReader(body))
	if err != nil {
		res.Err = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Rungated-Agent", c.Agent)
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	start := time.Now()
	resp, err := c.HTTP.Do(req)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	res.ClientUS = time.Since(start).Microseconds()
	if resp.StatusCode != 200 {
		res.Err = fmt.Sprintf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
		return res
	}
	var out struct {
		Computed string `json:"computed"`
		Score    int    `json:"score"`
		Latency  int64  `json:"latency_us"`
		Findings []struct {
			Detector string `json:"detector"`
			Rule     string `json:"rule"`
			Tier     int    `json:"tier"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		res.Err = err.Error()
		return res
	}
	res.Computed, res.Score, res.ServerUS = out.Computed, out.Score, out.Latency
	seen := map[string]bool{}
	for _, f := range out.Findings {
		k := f.Detector + ":" + f.Rule
		if !seen[k] {
			seen[k] = true
			res.Detectors = append(res.Detectors, k)
		}
	}
	sort.Strings(res.Detectors)
	return res
}

func Run(ctx context.Context, c *Client, cases []Case, concurrency int) []Result {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if concurrency <= 0 {
		concurrency = 8
	}
	out := make([]Result, len(cases))
	idx := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				out[i] = c.decide(ctx, cases[i])
			}
		}()
	}
	for i := range cases {
		idx <- i
	}
	close(idx)
	wg.Wait()
	return out
}
