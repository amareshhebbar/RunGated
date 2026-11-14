package tokenizer

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"unicode"
)

type charsMap struct {
	trie       []uint32
	normalized []byte
}

func newCharsMap(b64 string) (*charsMap, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(raw) < 4 {
		return nil, errors.New("tokenizer: charsmap too short")
	}
	size := int(binary.LittleEndian.Uint32(raw[:4]))
	if size%4 != 0 || 4+size > len(raw) {
		return nil, errors.New("tokenizer: charsmap trie size invalid")
	}
	trie := make([]uint32, size/4)
	for i := range trie {
		trie[i] = binary.LittleEndian.Uint32(raw[4+i*4:])
	}
	return &charsMap{trie: trie, normalized: raw[4+size:]}, nil
}

func dartsOffset(u uint32) uint32 { return (u >> 10) << ((u & (1 << 9)) >> 6) }
func dartsLabel(u uint32) uint32  { return u & ((1 << 31) | 0xFF) }
func dartsHasLeaf(u uint32) bool  { return (u>>8)&1 == 1 }
func dartsValue(u uint32) uint32  { return u & ((1 << 31) - 1) }

func (c *charsMap) prefixSearch(key []byte) []uint32 {
	var results []uint32
	if len(c.trie) == 0 {
		return nil
	}
	pos := uint32(0)
	unit := c.trie[pos]
	pos ^= dartsOffset(unit)
	for _, b := range key {
		if b == 0 {
			break
		}
		pos ^= uint32(b)
		if int(pos) >= len(c.trie) {
			return results
		}
		unit = c.trie[pos]
		if dartsLabel(unit) != uint32(b) {
			return results
		}
		pos ^= dartsOffset(unit)
		if dartsHasLeaf(unit) {
			if int(pos) >= len(c.trie) {
				return results
			}
			results = append(results, dartsValue(c.trie[pos]))
		}
	}
	return results
}

func (c *charsMap) transform(chunk string) (string, bool) {
	res := c.prefixSearch([]byte(chunk))
	if len(res) == 0 {
		return "", false
	}
	idx := int(res[0])
	end := idx
	for end < len(c.normalized) && c.normalized[end] != 0 {
		end++
	}
	if idx > len(c.normalized) {
		return "", false
	}
	return string(c.normalized[idx:end]), true
}

func isGraphemeExtend(r rune) bool {
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc) || r == 0x200d || (r >= 0xfe00 && r <= 0xfe0f) || (r >= 0x1f3fb && r <= 0x1f3ff) || (r >= 0xe0020 && r <= 0xe007f)
}

func isGraphemeControl(r rune) bool {
	if r == 0x200c || r == 0x200d || (r >= 0xe0020 && r <= 0xe007f) {
		return false
	}
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
}

func (c *charsMap) normalize(n NString) NString {
	out := NString{}
	i := 0
	for i < n.Len() {
		j := i + 1
		if n.R[i] == '\r' && j < n.Len() && n.R[j] == '\n' {
			j++
		} else {
			for j < n.Len() && !isGraphemeControl(n.R[j-1]) && (isGraphemeExtend(n.R[j]) || n.R[j-1] == 0x200d) {
				j++
			}
		}
		chunk := string(n.R[i:j])
		if len(chunk) < 6 {
			if rep, ok := c.transform(chunk); ok {
				n.replaceInto(&out, i, j, []rune(rep))
				i = j
				continue
			}
		}
		for k := i; k < j; k++ {
			part := string(n.R[k : k+1])
			if rep, ok := c.transform(part); ok {
				n.replaceInto(&out, k, k+1, []rune(rep))
			} else {
				out.push(n.R[k], n.S[k], n.E[k])
			}
		}
		i = j
	}
	return out
}
