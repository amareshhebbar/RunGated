package mcpproxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/jsonx"
)

type interceptReq struct {
	Direction string          `json:"direction"`
	SessionID string          `json:"session_id"`
	Message   json.RawMessage `json:"message"`
}

type interceptResp struct {
	Action  string          `json:"action"`
	Message json.RawMessage `json:"message,omitempty"`
}

func (p *Proxy) Intercept(w http.ResponseWriter, r *http.Request) {
	id, err := p.Resolver.Resolve(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
		return
	}
	name := r.PathValue("server")
	ag := p.Engine.Registry.Get(id.AgentID)
	if ag == nil || ag.Manifest.FindServer(name) == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown agent or mcp server"})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, p.MaxBody+1))
	if err != nil || int64(len(body)) > p.MaxBody {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "body too large"})
		return
	}
	var in interceptReq
	if err := json.Unmarshal(body, &in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
		return
	}
	msg, err := jsonx.Decode(in.Message)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "message is not a JSON-RPC object"})
		return
	}
	sc := Scope{Agent: id.AgentID, Session: id.SessionID, Server: name}
	if sc.Session == "" {
		sc.Session = in.SessionID
	}
	key := sc.Agent + "|" + sc.Server + "|stdio:" + in.SessionID
	switch in.Direction {
	case "client":
		ok, reply, corr := p.Interceptor.Client(r.Context(), sc, msg, nil)
		if !ok {
			writeJSON(w, 200, map[string]any{"action": "reply", "message": json.RawMessage(Encode(reply))})
			return
		}
		if IsRequest(msg) {
			p.setCorr(key, IDKey(msg), corr)
		}
		writeJSON(w, 200, map[string]any{"action": "forward", "message": json.RawMessage(Encode(msg))})
	case "server":
		out := msg
		if IsResponse(msg) {
			if corr, ok := p.takeCorr(key, IDKey(msg)); ok {
				out = p.Interceptor.Server(r.Context(), sc, corr, msg, nil)
			}
		}
		writeJSON(w, 200, map[string]any{"action": "forward", "message": json.RawMessage(Encode(out))})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "direction must be client or server"})
	}
}

type StdioConfig struct {
	Gateway string
	Server  string
	Agent   string
	Key     string
	Token   string
	Session string
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	HTTP    *http.Client
}

type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) write(b []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.w.Write(append(bytes.TrimRight(b, "\n"), '\n'))
}

func RunStdio(ctx context.Context, cfg StdioConfig) error {
	if len(cfg.Command) == 0 {
		return errors.New("no server command given")
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{}
	}
	if cfg.Session == "" {
		cfg.Session = core.NewID("stdio")
	}
	url := strings.TrimRight(cfg.Gateway, "/") + "/v1/mcp/" + cfg.Server + "/intercept"
	cmd := exec.CommandContext(ctx, cfg.Command[0], cfg.Command[1:]...)
	cmd.Stderr = cfg.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	childIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	toChild := &lineWriter{w: childIn}
	toClient := &lineWriter{w: cfg.Stdout}

	call := func(dir string, raw []byte) (*interceptResp, error) {
		body, _ := json.Marshal(interceptReq{Direction: dir, SessionID: cfg.Session, Message: raw})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if cfg.Token != "" {
			req.Header.Set("X-Rungated-Token", cfg.Token)
		} else {
			req.Header.Set("X-Rungated-Agent", cfg.Agent)
			if cfg.Key != "" {
				req.Header.Set("X-Rungated-Key", cfg.Key)
			}
		}
		req.Header.Set("X-Rungated-Session", cfg.Session)
		resp, err := cfg.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		}
		var out interceptResp
		if err := json.Unmarshal(b, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}

	failReply := func(raw []byte, err error) {
		m, perr := jsonx.Decode(raw)
		if perr != nil || (!IsRequest(m) && !IsResponse(m)) {
			return
		}
		toClient.write(Encode(ErrorReply(m, -32000, "rungated gateway unavailable: "+err.Error())))
	}

	var wg sync.WaitGroup
	outDone := make(chan struct{})
	go func() {
		defer close(outDone)
		rd := bufio.NewReaderSize(childOut, 1<<20)
		for {
			line, err := rd.ReadBytes('\n')
			if t := bytes.TrimSpace(line); len(t) > 0 {
				raw := append([]byte(nil), t...)
				wg.Add(1)
				go func() {
					defer wg.Done()
					res, cerr := call("server", raw)
					if cerr != nil {
						failReply(raw, cerr)
						return
					}
					toClient.write(res.Message)
				}()
			}
			if err != nil {
				return
			}
		}
	}()

	go func() {
		rd := bufio.NewReaderSize(cfg.Stdin, 1<<20)
		var iw sync.WaitGroup
		for {
			line, err := rd.ReadBytes('\n')
			if t := bytes.TrimSpace(line); len(t) > 0 {
				raw := append([]byte(nil), t...)
				iw.Add(1)
				go func() {
					defer iw.Done()
					res, cerr := call("client", raw)
					if cerr != nil {
						failReply(raw, cerr)
						return
					}
					switch res.Action {
					case "forward":
						toChild.write(res.Message)
					case "reply":
						toClient.write(res.Message)
					}
				}()
			}
			if err != nil {
				break
			}
		}
		iw.Wait()
		childIn.Close()
	}()

	<-outDone
	wg.Wait()
	return cmd.Wait()
}
