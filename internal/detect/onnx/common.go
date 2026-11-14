//go:build onnx

package onnx

import (
	"math"
	"unicode/utf8"

	"github.com/amareshhebbar/rungated/internal/tokenizer"
)

type window struct {
	text    string
	byteOff int
}

func makeWindows(tk *tokenizer.Tokenizer, text string, maxLen, stride, maxWindows int) []window {
	capacity := maxLen - tk.NumSpecial(false)
	if capacity < 8 {
		capacity = 8
	}
	enc := tk.Encode(text, false)
	n := enc.Len()
	if n <= capacity {
		return []window{{text: text}}
	}
	if stride >= capacity {
		stride = capacity / 4
	}
	step := capacity - stride
	var out []window
	for start := 0; start < n; start += step {
		end := start + capacity
		if end > n {
			end = n
		}
		a, b := enc.Offsets[start][0], enc.Offsets[end-1][1]
		if a < 0 {
			a = 0
		}
		if b > len(text) {
			b = len(text)
		}
		for a > 0 && a < len(text) && !utf8.RuneStart(text[a]) {
			a--
		}
		for b < len(text) && !utf8.RuneStart(text[b]) {
			b++
		}
		if b > a {
			out = append(out, window{text: text[a:b], byteOff: a})
		}
		if end == n {
			break
		}
	}
	if len(out) == 0 {
		return []window{{text: text}}
	}
	if maxWindows > 0 && len(out) > maxWindows {
		last := out[len(out)-1]
		out = append(out[:maxWindows-1:maxWindows-1], last)
	}
	return out
}

func seqFeeds(ids [][]int, typeIDs [][]int, padID int) map[string]feed {
	b := len(ids)
	l := 0
	for _, x := range ids {
		if len(x) > l {
			l = len(x)
		}
	}
	in := make([]int64, b*l)
	mask := make([]int64, b*l)
	tt := make([]int64, b*l)
	for i, x := range ids {
		for j := 0; j < l; j++ {
			if j < len(x) {
				in[i*l+j] = int64(x[j])
				mask[i*l+j] = 1
				if typeIDs != nil && j < len(typeIDs[i]) {
					tt[i*l+j] = int64(typeIDs[i][j])
				}
			} else {
				in[i*l+j] = int64(padID)
			}
		}
	}
	shape := []int64{int64(b), int64(l)}
	return map[string]feed{
		"input_ids":      {shape: shape, i64: in},
		"attention_mask": {shape: shape, i64: mask},
		"token_type_ids": {shape: shape, i64: tt},
	}
}

func softmax(x []float32) []float64 {
	m := x[0]
	for _, v := range x {
		if v > m {
			m = v
		}
	}
	out := make([]float64, len(x))
	var sum float64
	for i, v := range x {
		out[i] = math.Exp(float64(v - m))
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func sigmoid(v float32) float64 { return 1 / (1 + math.Exp(-float64(v))) }
