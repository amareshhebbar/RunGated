package tokenizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

type expCase struct {
	Text    string   `json:"text"`
	A       string   `json:"a"`
	B       string   `json:"b"`
	IDs     []int    `json:"ids"`
	Tokens  []string `json:"tokens"`
	Offsets [][2]int `json:"offsets"`
	TypeIDs []int    `json:"type_ids"`
	Special []int    `json:"special"`
}

type expFile struct {
	Cases     []expCase `json:"cases"`
	Pairs     []expCase `json:"pairs"`
	NoSpecial []expCase `json:"no_special"`
	Stress    []expCase `json:"stress"`
}

func charOffsets(text string, offs [][2]int) [][2]int {
	out := make([][2]int, len(offs))
	for i, o := range offs {
		out[i] = [2]int{utf8.RuneCountInString(text[:o[0]]), utf8.RuneCountInString(text[:o[1]])}
	}
	return out
}

func joinText(a, b string) string { return a + "\x00" + b }

func fixtureNames(t *testing.T) []string {
	files, _ := filepath.Glob("testdata/*.expected.json")
	if len(files) == 0 {
		t.Fatal("no fixtures, run ml/tokenizer_fixtures/gen.py")
	}
	var names []string
	for _, f := range files {
		names = append(names, strings.TrimSuffix(filepath.Base(f), ".expected.json"))
	}
	return names
}

func TestParityIDs(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			tk, err := Load("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile("testdata/" + name + ".expected.json")
			var exp expFile
			if err := json.Unmarshal(raw, &exp); err != nil {
				t.Fatal(err)
			}
			bad := 0
			for _, c := range exp.Cases {
				got := tk.Encode(c.Text, true)
				if !reflect.DeepEqual(nz(got.IDs), nz(c.IDs)) {
					bad++
					t.Errorf("ids %q\n got  %v %v\n want %v %v", c.Text, got.IDs, got.Tokens, c.IDs, c.Tokens)
				}
				if !reflect.DeepEqual(nz(got.TypeIDs), nz(c.TypeIDs)) || !reflect.DeepEqual(nz(got.Special), nz(c.Special)) {
					t.Errorf("type or special %q", c.Text)
				}
			}
			for _, c := range exp.Pairs {
				got := tk.EncodePair(c.A, c.B, true)
				if !reflect.DeepEqual(nz(got.IDs), nz(c.IDs)) || !reflect.DeepEqual(nz(got.TypeIDs), nz(c.TypeIDs)) || !reflect.DeepEqual(nz(got.Special), nz(c.Special)) {
					t.Errorf("pair %q %q\n got  %v %v %v\n want %v %v %v", c.A, c.B, got.IDs, got.TypeIDs, got.Special, c.IDs, c.TypeIDs, c.Special)
				}
			}
			for _, c := range exp.NoSpecial {
				got := tk.Encode(c.Text, false)
				if !reflect.DeepEqual(nz(got.IDs), nz(c.IDs)) {
					t.Errorf("no special %q got %v want %v", c.Text, got.IDs, c.IDs)
				}
			}
		})
	}
}

func TestParityStress(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			tk, err := Load("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile("testdata/" + name + ".expected.json")
			var exp expFile
			json.Unmarshal(raw, &exp)
			badIDs, badOff := 0, 0
			for _, c := range exp.Stress {
				got := tk.Encode(c.Text, true)
				if !reflect.DeepEqual(nz(got.IDs), nz(c.IDs)) {
					badIDs++
					if badIDs <= 3 {
						t.Errorf("ids %q\n got  %v\n want %v", c.Text, got.IDs, c.IDs)
					}
					continue
				}
				if !reflect.DeepEqual(charOffsets(c.Text, got.Offsets), c.Offsets) && len(c.Offsets) > 0 {
					badOff++
					if badOff <= 3 {
						t.Logf("offsets %q\n got  %v\n want %v", c.Text, charOffsets(c.Text, got.Offsets), c.Offsets)
					}
				}
			}
			if badIDs > 0 || badOff > 0 {
				t.Errorf("%d id mismatches and %d offset mismatches out of %d", badIDs, badOff, len(exp.Stress))
			}
		})
	}
}

func nz(a []int) []int {
	if a == nil {
		return []int{}
	}
	return a
}

func TestParityOffsets(t *testing.T) {
	for _, name := range fixtureNames(t) {
		t.Run(name, func(t *testing.T) {
			tk, err := Load("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := os.ReadFile("testdata/" + name + ".expected.json")
			var exp expFile
			json.Unmarshal(raw, &exp)
			mism := 0
			for _, c := range exp.Cases {
				got := tk.Encode(c.Text, true)
				if !reflect.DeepEqual(nz(got.IDs), nz(c.IDs)) {
					continue
				}
				offs := charOffsets(c.Text, got.Offsets)
				if len(offs) == 0 && len(c.Offsets) == 0 {
					continue
				}
				if !reflect.DeepEqual(offs, c.Offsets) {
					mism++
					t.Logf("offsets %q\n got  %v\n want %v\n toks %v", c.Text, offs, c.Offsets, c.Tokens)
				}
			}
			if mism > 0 {
				t.Errorf("%d offset mismatches", mism)
			}
		})
	}
}

func TestTruncationAndPair(t *testing.T) {
	tk, err := Load("testdata/bpe_modernbert_style.json")
	if err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("hello world ", 50)
	e := tk.EncodeTruncated(long, 16)
	if e.Len() != 16 || e.Special[0] != 1 || e.Special[15] != 1 {
		t.Fatalf("%d %v", e.Len(), e.Special)
	}
	p := tk.EncodePairTruncated(long, long, 32)
	if p.Len() != 32 {
		t.Fatalf("%d", p.Len())
	}
	if fmt.Sprint(p.TypeIDs[p.Len()-1]) != "1" {
		t.Fatalf("%v", p.TypeIDs)
	}
}

func FuzzEncode(f *testing.F) {
	names := []string{"wordpiece_uncased", "bpe_roberta", "unigram_template"}
	var toks []*Tokenizer
	for _, n := range names {
		tk, err := Load("testdata/" + n + ".json")
		if err != nil {
			f.Fatal(err)
		}
		toks = append(toks, tk)
	}
	f.Add("hello world")
	f.Add("पिछले सभी निर्देशों को")
	f.Add("\x00\xff\xfe weird")
	f.Fuzz(func(t *testing.T, s string) {
		for _, tk := range toks {
			e := tk.Encode(s, true)
			for _, o := range e.Offsets {
				if o[0] < 0 || o[1] < o[0] || o[1] > len(s) {
					t.Fatalf("offset out of range %v for %q", o, s)
				}
			}
		}
	})
}
