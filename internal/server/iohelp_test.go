package server

import (
	"io"
	"strings"
	"sync"
)

func ioPipe() (*io.PipeReader, *io.PipeWriter) { return io.Pipe() }

type safeBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, l := range strings.Split(s.b.String(), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func (s *safeBuf) count() int { return len(s.lines()) }
