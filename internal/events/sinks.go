package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/amareshhebbar/rungated/internal/core"
)

type Sink interface {
	Name() string
	Write(ctx context.Context, evs []core.Event) error
}

type Webhook struct {
	URL        string
	Secret     string
	Format     string
	Token      string
	MinVerdict string
	Client     *http.Client
}

func (w *Webhook) Name() string { return "webhook:" + w.Format }

func (w *Webhook) Write(ctx context.Context, evs []core.Event) error {
	var sel []core.Event
	min := core.Verdict(w.MinVerdict).Rank()
	for _, e := range evs {
		if w.MinVerdict == "" || (e.Decision != nil && (e.Decision.Computed.Rank() >= min || e.Decision.Verdict.Rank() >= min)) {
			sel = append(sel, e)
		}
	}
	if len(sel) == 0 {
		return nil
	}
	var body []byte
	switch w.Format {
	case "splunk":
		var b bytes.Buffer
		for _, e := range sel {
			x, _ := json.Marshal(map[string]any{"time": float64(e.Time.UnixMilli()) / 1000, "sourcetype": "rungated:event", "source": "rungated", "event": e})
			b.Write(x)
		}
		body = b.Bytes()
	default:
		body, _ = json.Marshal(map[string]any{"events": sel})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if w.Format == "splunk" && w.Token != "" {
		req.Header.Set("Authorization", "Splunk "+w.Token)
	} else if w.Token != "" {
		req.Header.Set("Authorization", "Bearer "+w.Token)
	}
	if w.Secret != "" {
		ts := fmt.Sprint(time.Now().Unix())
		m := hmac.New(sha256.New, []byte(w.Secret))
		m.Write([]byte(ts + "."))
		m.Write(body)
		req.Header.Set("X-Rungated-Timestamp", ts)
		req.Header.Set("X-Rungated-Signature", "v1="+hex.EncodeToString(m.Sum(nil)))
	}
	cl := w.Client
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook %s returned %d", w.URL, resp.StatusCode)
	}
	return nil
}
