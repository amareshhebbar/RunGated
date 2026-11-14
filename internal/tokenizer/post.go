package tokenizer

import (
	"encoding/json"
	"fmt"
	"unicode"
)

type Encoding struct {
	IDs     []int
	TypeIDs []int
	Attn    []int
	Special []int
	Tokens  []string
	Offsets [][2]int
}

func (e *Encoding) Len() int { return len(e.IDs) }

func (e *Encoding) add(id int, tokn string, off [2]int, typeID, special int) {
	e.IDs = append(e.IDs, id)
	e.Tokens = append(e.Tokens, tokn)
	e.Offsets = append(e.Offsets, off)
	e.TypeIDs = append(e.TypeIDs, typeID)
	e.Attn = append(e.Attn, 1)
	e.Special = append(e.Special, special)
}

func (e *Encoding) appendAll(o *Encoding, typeID int) {
	for i := range o.IDs {
		e.add(o.IDs[i], o.Tokens[i], o.Offsets[i], typeID, o.Special[i])
	}
}

type postProcessor interface {
	Process(a, b *Encoding, addSpecial bool) *Encoding
	NumSpecial(pair bool) int
}

func defaultMerge(a, b *Encoding) *Encoding {
	out := &Encoding{}
	out.appendAll(a, 0)
	if b != nil {
		out.appendAll(b, 1)
	}
	return out
}

type specialTok struct {
	id   int
	text string
}

type bertPost struct {
	sep, cls specialTok
}

func (p bertPost) NumSpecial(pair bool) int {
	if pair {
		return 3
	}
	return 2
}

func (p bertPost) Process(a, b *Encoding, addSpecial bool) *Encoding {
	if !addSpecial {
		return defaultMerge(a, b)
	}
	out := &Encoding{}
	out.add(p.cls.id, p.cls.text, [2]int{}, 0, 1)
	out.appendAll(a, 0)
	out.add(p.sep.id, p.sep.text, [2]int{}, 0, 1)
	if b != nil {
		out.appendAll(b, 1)
		out.add(p.sep.id, p.sep.text, [2]int{}, 1, 1)
	}
	return out
}

func trimByteLevelOffsets(e *Encoding, addPrefixSpace bool) *Encoding {
	out := &Encoding{IDs: e.IDs, TypeIDs: e.TypeIDs, Attn: e.Attn, Special: e.Special, Tokens: e.Tokens, Offsets: append([][2]int{}, e.Offsets...)}
	for i, t := range e.Tokens {
		rs := []rune(t)
		lead, trail := 0, 0
		for lead < len(rs) && (rs[lead] == 'Ġ' || unicode.IsSpace(rs[lead])) {
			lead++
		}
		for trail < len(rs) && (rs[len(rs)-1-trail] == 'Ġ' || unicode.IsSpace(rs[len(rs)-1-trail])) {
			trail++
		}
		off := out.Offsets[i]
		if lead > 0 {
			isFirst := i == 0 || off[0] == 0
			l := lead
			if isFirst && addPrefixSpace && l == 1 {
				l = 0
			}
			off[0] += l
			if off[0] > off[1] {
				off[0] = off[1]
			}
		}
		if trail > 0 && off[1] >= trail && off[1]-trail > off[0] {
			off[1] -= trail
		}
		out.Offsets[i] = off
	}
	return out
}

type robertaPost struct {
	sep, cls specialTok
	trim     bool
	addSpace bool
}

func (p robertaPost) NumSpecial(pair bool) int {
	if pair {
		return 4
	}
	return 2
}

func (p robertaPost) Process(a, b *Encoding, addSpecial bool) *Encoding {
	if p.trim {
		a = trimByteLevelOffsets(a, p.addSpace)
		if b != nil {
			b = trimByteLevelOffsets(b, p.addSpace)
		}
	}
	if !addSpecial {
		out := &Encoding{}
		out.appendAll(a, 0)
		if b != nil {
			out.appendAll(b, 0)
		}
		return out
	}
	out := &Encoding{}
	out.add(p.cls.id, p.cls.text, [2]int{}, 0, 1)
	out.appendAll(a, 0)
	out.add(p.sep.id, p.sep.text, [2]int{}, 0, 1)
	if b != nil {
		out.add(p.sep.id, p.sep.text, [2]int{}, 0, 1)
		out.appendAll(b, 0)
		out.add(p.sep.id, p.sep.text, [2]int{}, 0, 1)
	}
	return out
}

type byteLevelPost struct {
	trim     bool
	addSpace bool
}

func (p byteLevelPost) NumSpecial(bool) int { return 0 }

func (p byteLevelPost) Process(a, b *Encoding, addSpecial bool) *Encoding {
	if p.trim {
		a = trimByteLevelOffsets(a, p.addSpace)
		if b != nil {
			b = trimByteLevelOffsets(b, p.addSpace)
		}
	}
	out := &Encoding{}
	out.appendAll(a, 0)
	if b != nil {
		out.appendAll(b, 0)
	}
	return out
}

type tmplItem struct {
	special bool
	id      string
	seqB    bool
	typeID  int
}

type templatePost struct {
	single, pair []tmplItem
	specials     map[string][]specialTok
}

func (p templatePost) count(items []tmplItem) int {
	n := 0
	for _, it := range items {
		if it.special {
			n += len(p.specials[it.id])
		}
	}
	return n
}

func (p templatePost) NumSpecial(pair bool) int {
	if pair {
		return p.count(p.pair)
	}
	return p.count(p.single)
}

func (p templatePost) Process(a, b *Encoding, addSpecial bool) *Encoding {
	if !addSpecial {
		return defaultMerge(a, b)
	}
	items := p.single
	if b != nil {
		items = p.pair
	}
	out := &Encoding{}
	for _, it := range items {
		if it.special {
			for _, st := range p.specials[it.id] {
				out.add(st.id, st.text, [2]int{}, it.typeID, 1)
			}
			continue
		}
		src := a
		if it.seqB {
			src = b
		}
		if src != nil {
			out.appendAll(src, it.typeID)
		}
	}
	return out
}

type seqPost []postProcessor

func (s seqPost) NumSpecial(pair bool) int {
	n := 0
	for _, p := range s {
		n += p.NumSpecial(pair)
	}
	return n
}

func (s seqPost) Process(a, b *Encoding, addSpecial bool) *Encoding {
	if len(s) == 0 {
		return defaultMerge(a, b)
	}
	cur := a
	var pr *Encoding = b
	for i, p := range s {
		out := p.Process(cur, pr, addSpecial)
		cur = out
		pr = nil
		_ = i
	}
	return cur
}

func parseSpecialPair(raw json.RawMessage) (specialTok, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) != 2 {
		return specialTok{}, fmt.Errorf("tokenizer: bad special token pair")
	}
	var s specialTok
	json.Unmarshal(arr[0], &s.text)
	json.Unmarshal(arr[1], &s.id)
	return s, nil
}

func parsePostProcessor(raw json.RawMessage) (postProcessor, error) {
	o, err := parseObj(raw)
	if err != nil || o == nil {
		return nil, err
	}
	switch t := o.str("type"); t {
	case "BertProcessing", "RobertaProcessing":
		sep, err := parseSpecialPair(o["sep"])
		if err != nil {
			return nil, err
		}
		cls, err := parseSpecialPair(o["cls"])
		if err != nil {
			return nil, err
		}
		if t == "BertProcessing" {
			return bertPost{sep: sep, cls: cls}, nil
		}
		return robertaPost{sep: sep, cls: cls, trim: o.boolean("trim_offsets", true), addSpace: o.boolean("add_prefix_space", true)}, nil
	case "ByteLevel":
		return byteLevelPost{trim: o.boolean("trim_offsets", true), addSpace: o.boolean("add_prefix_space", true)}, nil
	case "TemplateProcessing":
		p := templatePost{specials: map[string][]specialTok{}}
		parse := func(key string) ([]tmplItem, error) {
			var items []json.RawMessage
			if err := json.Unmarshal(o[key], &items); err != nil {
				return nil, err
			}
			var out []tmplItem
			for _, it := range items {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(it, &m); err != nil {
					return nil, err
				}
				if sp, ok := m["SpecialToken"]; ok {
					var v struct {
						ID     string `json:"id"`
						TypeID int    `json:"type_id"`
					}
					json.Unmarshal(sp, &v)
					out = append(out, tmplItem{special: true, id: v.ID, typeID: v.TypeID})
				} else if sq, ok := m["Sequence"]; ok {
					var v struct {
						ID     string `json:"id"`
						TypeID int    `json:"type_id"`
					}
					json.Unmarshal(sq, &v)
					out = append(out, tmplItem{seqB: v.ID == "B", typeID: v.TypeID})
				}
			}
			return out, nil
		}
		var err error
		if p.single, err = parse("single"); err != nil {
			return nil, err
		}
		if p.pair, err = parse("pair"); err != nil {
			return nil, err
		}
		var sts map[string]struct {
			IDs    []int    `json:"ids"`
			Tokens []string `json:"tokens"`
		}
		if err := json.Unmarshal(o["special_tokens"], &sts); err != nil {
			return nil, err
		}
		for k, v := range sts {
			for i := range v.IDs {
				p.specials[k] = append(p.specials[k], specialTok{id: v.IDs[i], text: v.Tokens[i]})
			}
		}
		return p, nil
	case "Sequence":
		var items []json.RawMessage
		if err := json.Unmarshal(o["processors"], &items); err != nil {
			return nil, err
		}
		var seq seqPost
		for _, it := range items {
			p, err := parsePostProcessor(it)
			if err != nil {
				return nil, err
			}
			if p != nil {
				seq = append(seq, p)
			}
		}
		return seq, nil
	default:
		return nil, fmt.Errorf("tokenizer: unsupported post processor %q", t)
	}
}
