package sse

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
)

type Event struct {
	Event   string
	Data    string
	ID      string
	Retry   string
	Comment string
}

type Reader struct {
	r *bufio.Reader
}

func NewReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReaderSize(r, 64*1024)}
}

func (r *Reader) Next() (*Event, error) {
	ev := &Event{}
	var data []string
	seen := false
	for {
		line, err := r.r.ReadString('\n')
		if err != nil && line == "" {
			if seen {
				ev.Data = strings.Join(data, "\n")
				return ev, nil
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if seen {
				ev.Data = strings.Join(data, "\n")
				return ev, nil
			}
			if err != nil {
				return nil, err
			}
			continue
		}
		seen = true
		if strings.HasPrefix(line, ":") {
			ev.Comment = strings.TrimSpace(line[1:])
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.Event = value
		case "data":
			data = append(data, value)
		case "id":
			ev.ID = value
		case "retry":
			ev.Retry = value
		}
		if err != nil {
			ev.Data = strings.Join(data, "\n")
			return ev, nil
		}
	}
}

type Writer struct {
	mu sync.Mutex
	w  http.ResponseWriter
	f  http.Flusher
}

func NewWriter(w http.ResponseWriter) *Writer {
	f, _ := w.(http.Flusher)
	return &Writer{w: w, f: f}
}

func (s *Writer) Write(ev *Event) error {
	var b bytes.Buffer
	if ev.Comment != "" && ev.Data == "" && ev.Event == "" {
		b.WriteString(": " + ev.Comment + "\n\n")
	} else {
		if ev.ID != "" {
			b.WriteString("id: " + ev.ID + "\n")
		}
		if ev.Event != "" {
			b.WriteString("event: " + ev.Event + "\n")
		}
		if ev.Retry != "" {
			b.WriteString("retry: " + ev.Retry + "\n")
		}
		for _, l := range strings.Split(ev.Data, "\n") {
			b.WriteString("data: " + l + "\n")
		}
		b.WriteString("\n")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(b.Bytes()); err != nil {
		return err
	}
	if s.f != nil {
		s.f.Flush()
	}
	return nil
}

func (s *Writer) Comment(c string) error {
	return s.Write(&Event{Comment: c})
}
