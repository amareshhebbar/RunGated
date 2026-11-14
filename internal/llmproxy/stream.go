package llmproxy

import (
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/sse"
	"github.com/amareshhebbar/rungated/internal/vault"
)

type streamHandler interface {
	Event(ev *sse.Event) bool
	End()
}

type stream struct {
	g         *guard
	raw       strings.Builder
	checked   int
	in, out   int
	responded bool
	blocked   string
	rest      map[string]*vault.StreamRestorer
}

func newStream(g *guard) *stream {
	return &stream{g: g, rest: map[string]*vault.StreamRestorer{}}
}

func (s *stream) restorer(k string) *vault.StreamRestorer {
	r := s.rest[k]
	if r == nil {
		r = s.g.p.Engine.Vault.NewStreamRestorer(s.g.tenant, s.g.id.SessionID)
		s.rest[k] = r
	}
	return s.rest[k]
}

func (s *stream) texts() []core.TextPart {
	return []core.TextPart{{Source: core.SourceAssistant, Text: s.raw.String()}}
}

func (s *stream) feed(text string) bool {
	s.raw.WriteString(text)
	if s.responded || s.raw.Len()-s.checked < 256 {
		return false
	}
	s.checked = s.raw.Len()
	if !s.g.cheap(s.raw.String()) {
		return false
	}
	o := s.g.decideResponse(s.texts(), s.in, s.out)
	s.responded = true
	if !o.Allowed {
		s.blocked = o.Message()
		if s.blocked == "" {
			s.blocked = "Rungated stopped this response: " + o.Decision.Summary()
		}
		return true
	}
	return false
}

func (s *stream) finish() {
	if s.responded {
		return
	}
	s.responded = true
	s.g.decideResponse(s.texts(), s.in, s.out)
}

func (g *guard) runStream(body io.Reader, injectedUsage bool) {
	var h streamHandler
	st := newStream(g)
	switch g.api {
	case APIChat:
		h = &chatStream{stream: st, tools: map[int]map[int]*bufCall{}, finished: map[int]bool{}, injected: injectedUsage}
	case APIResponses:
		h = &respStream{stream: st, buf: map[int]*fcBuf{}, replaced: map[int]map[string]any{}, argsOver: map[int]string{}, texts: map[string]*strings.Builder{}}
	default:
		h = &msgStream{stream: st, tools: map[int]*tuBuf{}}
	}
	rd := sse.NewReader(body)
	for {
		ev, err := rd.Next()
		if err != nil {
			break
		}
		if h.Event(ev) {
			break
		}
	}
	h.End()
}

type bufCall struct {
	id   string
	name string
	args strings.Builder
}

type chatStream struct {
	*stream
	base     map[string]any
	tools    map[int]map[int]*bufCall
	finished map[int]bool
	injected bool
	sawDone  bool
	stopped  bool
}

func (c *chatStream) chunk(ci int, delta map[string]any, finish any) {
	m := map[string]any{"object": "chat.completion.chunk"}
	for _, k := range []string{"id", "created", "model", "system_fingerprint", "service_tier"} {
		if v, ok := c.base[k]; ok {
			m[k] = v
		}
	}
	m["choices"] = []any{map[string]any{"index": ci, "delta": delta, "finish_reason": finish}}
	c.g.w.Write(&sse.Event{Data: string(marshal(m))})
}

func (c *chatStream) flushChoice(ci int, finish string) string {
	if t := c.restorer(ck(ci)).Flush(); t != "" {
		c.chunk(ci, map[string]any{"content": t}, nil)
	}
	calls := c.tools[ci]
	delete(c.tools, ci)
	idxs := make([]int, 0, len(calls))
	for i := range calls {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	kept := 0
	var notes []string
	for _, i := range idxs {
		bc := calls[i]
		args := bc.args.String()
		ok, msg, newArgs, changed := c.g.call(bc.name, parseArgs(args), args)
		if !ok {
			notes = append(notes, msg)
			continue
		}
		if changed {
			args = string(marshal(newArgs))
		}
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		c.chunk(ci, map[string]any{"tool_calls": []any{map[string]any{"index": kept, "id": bc.id, "type": "function", "function": map[string]any{"name": bc.name, "arguments": args}}}}, nil)
		kept++
	}
	if len(notes) > 0 {
		c.chunk(ci, map[string]any{"content": strings.Join(notes, "\n")}, nil)
	}
	if len(idxs) > 0 && kept == 0 && finish == "tool_calls" {
		finish = "stop"
	}
	return finish
}

func ck(i int) string { return "c" + strconv.Itoa(i) }

func (c *chatStream) terminate() {
	c.stopped = true
	c.chunk(0, map[string]any{"content": c.blocked}, nil)
	c.chunk(0, map[string]any{}, "stop")
	c.g.w.Write(&sse.Event{Data: "[DONE]"})
}

func (c *chatStream) Event(ev *sse.Event) bool {
	if ev.Data == "" {
		c.g.w.Write(ev)
		return false
	}
	if ev.Data == "[DONE]" {
		c.sawDone = true
		c.flushOpen()
		c.g.w.Write(ev)
		return false
	}
	m, err := decode([]byte(ev.Data))
	if err != nil {
		c.g.w.Write(ev)
		return false
	}
	if c.base == nil {
		c.base = m
	}
	hasUsage := false
	if u := obj(m["usage"]); u != nil {
		hasUsage = true
		c.in, c.out = num(u["prompt_tokens"]), num(u["completion_tokens"])
	}
	choices := arr(m["choices"])
	if len(choices) == 0 {
		if hasUsage && c.injected {
			return false
		}
		c.g.w.Write(ev)
		return false
	}
	kept := make([]any, 0, len(choices))
	for _, cv := range choices {
		ch := obj(cv)
		ci := num(ch["index"])
		delta := obj(ch["delta"])
		if delta != nil {
			if s, ok := delta["content"].(string); ok && s != "" {
				if c.feed(s) {
					c.terminate()
					return true
				}
				if r := c.restorer(ck(ci)).Push(s); r == "" {
					delete(delta, "content")
				} else {
					delta["content"] = r
				}
			}
			if tcs := arr(delta["tool_calls"]); len(tcs) > 0 {
				if c.tools[ci] == nil {
					c.tools[ci] = map[int]*bufCall{}
				}
				for _, tv := range tcs {
					tm := obj(tv)
					idx := num(tm["index"])
					bc := c.tools[ci][idx]
					if bc == nil {
						bc = &bufCall{}
						c.tools[ci][idx] = bc
					}
					if id := str(tm, "id"); id != "" {
						bc.id = id
					}
					fn := obj(tm["function"])
					if n := str(fn, "name"); n != "" {
						bc.name += n
					}
					bc.args.WriteString(str(fn, "arguments"))
				}
				delete(delta, "tool_calls")
			}
		}
		fr, _ := ch["finish_reason"].(string)
		if fr != "" {
			ch["finish_reason"] = c.flushChoice(ci, fr)
			c.finished[ci] = true
		}
		if fr == "" && len(delta) == 0 {
			continue
		}
		kept = append(kept, cv)
	}
	if len(kept) == 0 && !hasUsage {
		return false
	}
	m["choices"] = kept
	c.g.w.Write(&sse.Event{Data: string(marshal(m))})
	return false
}

func (c *chatStream) flushOpen() {
	seen := map[int]bool{}
	for k := range c.rest {
		seen[atoi(strings.TrimPrefix(k, "c"))] = true
	}
	for k := range c.tools {
		seen[k] = true
	}
	var cis []int
	for k := range seen {
		if !c.finished[k] {
			cis = append(cis, k)
		}
	}
	sort.Ints(cis)
	for _, ci := range cis {
		c.flushChoice(ci, "")
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func (c *chatStream) End() {
	if !c.sawDone && !c.stopped {
		c.flushOpen()
	}
	c.finish()
}

type fcBuf struct {
	added      map[string]any
	firstDelta map[string]any
	argsDone   map[string]any
	args       strings.Builder
}

type respStream struct {
	*stream
	seq      int
	buf      map[int]*fcBuf
	replaced map[int]map[string]any
	argsOver map[int]string
	texts    map[string]*strings.Builder
	created  map[string]any
	stopped  bool
	curItem  string
	curIdx   int
}

func (r *respStream) emit(m map[string]any) {
	if _, ok := m["sequence_number"]; ok {
		m["sequence_number"] = r.seq
		r.seq++
	}
	r.g.w.Write(&sse.Event{Event: str(m, "type"), Data: string(marshal(m))})
}

func (r *respStream) messageEvents(idx int, text string) map[string]any {
	item := messageItem(text)
	id := str(item, "id")
	inProg := map[string]any{"type": "message", "id": id, "status": "in_progress", "role": "assistant", "content": []any{}}
	part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
	full := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
	r.emit(map[string]any{"type": "response.output_item.added", "output_index": idx, "sequence_number": 0, "item": inProg})
	r.emit(map[string]any{"type": "response.content_part.added", "item_id": id, "output_index": idx, "content_index": 0, "sequence_number": 0, "part": part})
	r.emit(map[string]any{"type": "response.output_text.delta", "item_id": id, "output_index": idx, "content_index": 0, "sequence_number": 0, "delta": text, "logprobs": []any{}})
	r.emit(map[string]any{"type": "response.output_text.done", "item_id": id, "output_index": idx, "content_index": 0, "sequence_number": 0, "text": text, "logprobs": []any{}})
	r.emit(map[string]any{"type": "response.content_part.done", "item_id": id, "output_index": idx, "content_index": 0, "sequence_number": 0, "part": full})
	r.emit(map[string]any{"type": "response.output_item.done", "output_index": idx, "sequence_number": 0, "item": item})
	return item
}

func (r *respStream) resolve(idx int, b *fcBuf, done map[string]any) {
	item := obj(done["item"])
	argsText := str(item, "arguments")
	if argsText == "" {
		argsText = b.args.String()
	}
	ok, msg, newArgs, changed := r.g.call(str(item, "name"), parseArgs(argsText), argsText)
	if !ok {
		r.replaced[idx] = r.messageEvents(idx, msg)
		return
	}
	if changed {
		argsText = string(marshal(newArgs))
		r.argsOver[idx] = argsText
	}
	r.emit(b.added)
	if b.firstDelta != nil {
		b.firstDelta["delta"] = argsText
		r.emit(b.firstDelta)
	}
	if b.argsDone != nil {
		b.argsDone["arguments"] = argsText
		r.emit(b.argsDone)
	}
	item["arguments"] = argsText
	r.emit(done)
}

func (r *respStream) terminate() {
	r.stopped = true
	idx := r.curIdx
	if r.curItem != "" {
		r.emit(map[string]any{"type": "response.output_text.delta", "item_id": r.curItem, "output_index": idx, "content_index": 0, "sequence_number": 0, "delta": "\n\n" + r.blocked, "logprobs": []any{}})
	}
	resp := map[string]any{}
	if r.created != nil {
		for k, v := range r.created {
			resp[k] = v
		}
	}
	resp["status"] = "completed"
	resp["output"] = []any{messageItem(r.blocked)}
	r.emit(map[string]any{"type": "response.completed", "sequence_number": 0, "response": resp})
}

func (r *respStream) Event(ev *sse.Event) bool {
	if ev.Data == "" || ev.Data == "[DONE]" {
		r.g.w.Write(ev)
		return false
	}
	m, err := decode([]byte(ev.Data))
	if err != nil {
		r.g.w.Write(ev)
		return false
	}
	typ := str(m, "type")
	if typ == "" {
		typ = ev.Event
		m["type"] = typ
	}
	idx := num(m["output_index"])
	switch typ {
	case "response.created":
		r.created = obj(m["response"])
		r.emit(m)
	case "response.output_item.added":
		if str(obj(m["item"]), "type") == "function_call" {
			r.buf[idx] = &fcBuf{added: m}
			return false
		}
		r.emit(m)
	case "response.function_call_arguments.delta":
		if b := r.buf[idx]; b != nil {
			if b.firstDelta == nil {
				b.firstDelta = m
			}
			b.args.WriteString(str(m, "delta"))
			return false
		}
		r.emit(m)
	case "response.function_call_arguments.done":
		if b := r.buf[idx]; b != nil {
			b.argsDone = m
			if a := str(m, "arguments"); a != "" {
				b.args.Reset()
				b.args.WriteString(a)
			}
			return false
		}
		r.emit(m)
	case "response.output_item.done":
		item := obj(m["item"])
		if str(item, "type") == "function_call" {
			if b := r.buf[idx]; b != nil {
				r.resolve(idx, b, m)
				delete(r.buf, idx)
				return false
			}
		}
		if str(item, "type") == "message" {
			for _, pv := range arr(item["content"]) {
				pm := obj(pv)
				if t, ok := pm["text"].(string); ok {
					pm["text"] = r.g.restore(t)
				}
			}
		}
		r.emit(m)
	case "response.output_text.delta":
		d := str(m, "delta")
		r.curItem, r.curIdx = str(m, "item_id"), idx
		if r.feed(d) {
			r.terminate()
			return true
		}
		key := str(m, "item_id") + "/" + strconv.Itoa(num(m["content_index"]))
		rest := r.restorer(key).Push(d)
		if r.texts[key] == nil {
			r.texts[key] = &strings.Builder{}
		}
		if rest == "" {
			return false
		}
		r.texts[key].WriteString(rest)
		m["delta"] = rest
		r.emit(m)
	case "response.output_text.done":
		key := str(m, "item_id") + "/" + strconv.Itoa(num(m["content_index"]))
		if rs := r.rest[key]; rs != nil {
			if tail := rs.Flush(); tail != "" {
				if r.texts[key] == nil {
					r.texts[key] = &strings.Builder{}
				}
				r.texts[key].WriteString(tail)
				r.emit(map[string]any{"type": "response.output_text.delta", "item_id": m["item_id"], "output_index": m["output_index"], "content_index": m["content_index"], "sequence_number": 0, "delta": tail, "logprobs": []any{}})
			}
		}
		if b := r.texts[key]; b != nil {
			m["text"] = b.String()
		} else if t, ok := m["text"].(string); ok {
			m["text"] = r.g.restore(t)
		}
		r.emit(m)
	case "response.content_part.done":
		if pm := obj(m["part"]); pm != nil {
			if t, ok := pm["text"].(string); ok {
				pm["text"] = r.g.restore(t)
			}
		}
		r.emit(m)
	case "response.completed", "response.incomplete", "response.failed":
		resp := obj(m["response"])
		if u := obj(resp["usage"]); u != nil {
			r.in, r.out = num(u["input_tokens"]), num(u["output_tokens"])
		}
		items := arr(resp["output"])
		for i, iv := range items {
			it := obj(iv)
			switch str(it, "type") {
			case "function_call":
				if rep, ok := r.replaced[i]; ok {
					items[i] = rep
				} else if a, ok := r.argsOver[i]; ok {
					it["arguments"] = a
				}
			case "message":
				for _, pv := range arr(it["content"]) {
					pm := obj(pv)
					if t, ok := pm["text"].(string); ok {
						pm["text"] = r.g.restore(t)
					}
				}
			}
		}
		r.emit(m)
	default:
		r.emit(m)
	}
	return false
}

func (r *respStream) End() {
	r.finish()
}

type tuBuf struct {
	start map[string]any
	json  strings.Builder
}

type msgStream struct {
	*stream
	tools   map[int]*tuBuf
	kept    int
	seen    int
	curText int
	stopped bool
}

func (s *msgStream) emit(m map[string]any) {
	s.g.w.Write(&sse.Event{Event: str(m, "type"), Data: string(marshal(m))})
}

func (s *msgStream) textDelta(idx int, text string) {
	s.emit(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "text_delta", "text": text}})
}

func (s *msgStream) terminate() {
	s.stopped = true
	s.textDelta(s.curText, "\n\n"+s.blocked)
	s.emit(map[string]any{"type": "content_block_stop", "index": s.curText})
	s.emit(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": map[string]any{"output_tokens": s.out}})
	s.emit(map[string]any{"type": "message_stop"})
}

func (s *msgStream) resolve(idx int, tb *tuBuf) {
	cb := obj(tb.start["content_block"])
	raw := tb.json.String()
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	ok, msg, newArgs, changed := s.g.call(str(cb, "name"), parseArgs(raw), raw)
	if !ok {
		s.emit(map[string]any{"type": "content_block_start", "index": idx, "content_block": map[string]any{"type": "text", "text": ""}})
		s.textDelta(idx, msg)
		s.emit(map[string]any{"type": "content_block_stop", "index": idx})
		return
	}
	if changed {
		raw = string(marshal(newArgs))
	}
	cb["input"] = map[string]any{}
	s.emit(tb.start)
	s.emit(map[string]any{"type": "content_block_delta", "index": idx, "delta": map[string]any{"type": "input_json_delta", "partial_json": raw}})
	s.emit(map[string]any{"type": "content_block_stop", "index": idx})
	s.kept++
}

func (s *msgStream) Event(ev *sse.Event) bool {
	if ev.Data == "" {
		s.g.w.Write(ev)
		return false
	}
	m, err := decode([]byte(ev.Data))
	if err != nil {
		s.g.w.Write(ev)
		return false
	}
	typ := str(m, "type")
	if typ == "" {
		typ = ev.Event
		m["type"] = typ
	}
	idx := num(m["index"])
	switch typ {
	case "message_start":
		if u := obj(obj(m["message"])["usage"]); u != nil {
			s.in, s.out = num(u["input_tokens"]), num(u["output_tokens"])
		}
		s.emit(m)
	case "content_block_start":
		cb := obj(m["content_block"])
		switch str(cb, "type") {
		case "tool_use":
			s.tools[idx] = &tuBuf{start: m}
			s.seen++
			return false
		case "text":
			s.curText = idx
			if t := str(cb, "text"); t != "" {
				cb["text"] = s.g.restore(t)
			}
		}
		s.emit(m)
	case "content_block_delta":
		d := obj(m["delta"])
		switch str(d, "type") {
		case "text_delta":
			t := str(d, "text")
			s.curText = idx
			if s.feed(t) {
				s.terminate()
				return true
			}
			r := s.restorer(strconv.Itoa(idx)).Push(t)
			if r == "" {
				return false
			}
			d["text"] = r
		case "input_json_delta":
			if tb := s.tools[idx]; tb != nil {
				tb.json.WriteString(str(d, "partial_json"))
				return false
			}
		}
		s.emit(m)
	case "content_block_stop":
		if tb := s.tools[idx]; tb != nil {
			s.resolve(idx, tb)
			delete(s.tools, idx)
			return false
		}
		if rs := s.rest[strconv.Itoa(idx)]; rs != nil {
			if tail := rs.Flush(); tail != "" {
				s.textDelta(idx, tail)
			}
		}
		s.emit(m)
	case "message_delta":
		if u := obj(m["usage"]); u != nil {
			if n := num(u["output_tokens"]); n > 0 {
				s.out = n
			}
			if n := num(u["input_tokens"]); n > 0 {
				s.in = n
			}
		}
		if d := obj(m["delta"]); d != nil {
			if sr, _ := d["stop_reason"].(string); sr == "tool_use" && s.seen > 0 && s.kept == 0 {
				d["stop_reason"] = "end_turn"
			}
		}
		s.emit(m)
	default:
		s.emit(m)
	}
	return false
}

func (s *msgStream) End() {
	s.finish()
}
