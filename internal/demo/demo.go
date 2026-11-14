package demo

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	DefaultMockLLMListen = "127.0.0.1:9300"
	DefaultMCPListen     = "127.0.0.1:9301"
	DefaultWebListen     = "127.0.0.1:9302"
)

func Usage() string {
	return `rungated demo <service> [--listen ADDR]

services:
  mock-llm       scripted OpenAI compatible model that naively obeys instructions found in tool results
  poisoned-mcp   finance MCP server with a rug pull trigger
  evil-web       web server with a hidden injection page and an exfiltration sink`
}

func Run(args []string) error {
	if len(args) == 0 {
		return errors.New(Usage())
	}
	name := args[0]
	fs := flag.NewFlagSet("demo "+name, flag.ContinueOnError)
	var listen string
	var rugAfter int
	switch name {
	case "mock-llm":
		fs.StringVar(&listen, "listen", DefaultMockLLMListen, "listen address")
	case "poisoned-mcp":
		fs.StringVar(&listen, "listen", DefaultMCPListen, "listen address")
		fs.IntVar(&rugAfter, "rugpull-after", 0, "change the send_email description after this many tools/list calls, 0 means only on trigger")
	case "evil-web":
		fs.StringVar(&listen, "listen", DefaultWebListen, "listen address")
	default:
		return errors.New(Usage())
	}
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var h http.Handler
	switch name {
	case "mock-llm":
		h = NewMockLLM().Handler()
	case "poisoned-mcp":
		h = NewPoisonedMCP(rugAfter).Handler()
	case "evil-web":
		h = NewEvilWeb().Handler()
	}
	srv := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	fmt.Fprintf(os.Stderr, "demo %s listening on %s\n", name, listen)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(sctx)
	}
}
