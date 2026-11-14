package sse

import (
	"strings"
	"testing"
)

func TestReaderBasic(t *testing.T) {
	in := ": hi\n\nevent: a\ndata: one\ndata: two\n\ndata: [DONE]\n\n"
	r := NewReader(strings.NewReader(in))
	var got []Event
	for {
		ev, err := r.Next()
		if err != nil {
			break
		}
		got = append(got, *ev)
	}
	if len(got) != 3 || got[0].Comment != "hi" || got[1].Event != "a" || got[1].Data != "one\ntwo" || got[2].Data != "[DONE]" {
		t.Fatalf("unexpected parse: %+v", got)
	}
}

func FuzzSSE(f *testing.F) {
	f.Add("event: x\ndata: y\n\n")
	f.Add(": c\n\n\n\ndata\n")
	f.Add("data: a\r\ndata: b\r\n\r\n")
	f.Fuzz(func(t *testing.T, in string) {
		r := NewReader(strings.NewReader(in))
		for i := 0; i < len(in)+2; i++ {
			if _, err := r.Next(); err != nil {
				return
			}
		}
	})
}
