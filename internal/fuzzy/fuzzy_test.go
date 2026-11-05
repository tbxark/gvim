package fuzzy

import (
	"slices"
	"testing"
)

func TestScoreTable(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		target  string
		ok      bool
		indexes []int
	}{
		{name: "exact", query: "main.go", target: "main.go", ok: true, indexes: []int{0, 1, 2, 3, 4, 5, 6}},
		{name: "prefix", query: "ma", target: "main.go", ok: true, indexes: []int{0, 1}},
		{name: "contiguous interior", query: "ain", target: "main.go", ok: true, indexes: []int{1, 2, 3}},
		{name: "case insensitive", query: "SAVE", target: "save file", ok: true, indexes: []int{0, 1, 2, 3}},
		{name: "word boundary initials", query: "sf", target: "Save File", ok: true, indexes: []int{0, 5}},
		{name: "camel case boundary", query: "sf", target: "saveFile", ok: true, indexes: []int{0, 4}},
		{name: "gap skips one rune", query: "ac", target: "abc", ok: true, indexes: []int{0, 2}},
		{name: "rune indexes not bytes", query: "é", target: "café", ok: true, indexes: []int{3}},
		{name: "rune indexes after multibyte", query: "hl", target: "héllo", ok: true, indexes: []int{0, 2}},
		{name: "no match", query: "xyz", target: "Save File", ok: false},
		{name: "query longer than target", query: "abcd", target: "abc", ok: false},
		{name: "order matters for one term", query: "ba", target: "ab", ok: false},
		{name: "empty query matches", query: "", target: "anything", ok: true},
		{name: "blank query matches", query: "   ", target: "anything", ok: true},
		{name: "empty target no match", query: "a", target: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, indexes, ok := Score(tt.query, tt.target)
			if ok != tt.ok {
				t.Fatalf("Score(%q, %q) ok = %v, want %v (score %d)", tt.query, tt.target, ok, tt.ok, score)
			}
			if !ok {
				if score != 0 || indexes != nil {
					t.Errorf("Score(%q, %q) on no match = %d, %v; want 0, nil", tt.query, tt.target, score, indexes)
				}
				return
			}
			if !slices.Equal(indexes, tt.indexes) {
				t.Errorf("Score(%q, %q) indexes = %v, want %v", tt.query, tt.target, indexes, tt.indexes)
			}
		})
	}
}

func TestScoreEmptyQueryIsZero(t *testing.T) {
	for _, q := range []string{"", "  \t"} {
		score, indexes, ok := Score(q, "anything")
		if !ok || score != 0 || indexes != nil {
			t.Errorf("Score(%q) = %d, %v, %v; want 0, nil, true", q, score, indexes, ok)
		}
	}
}

func TestScoreOrdering(t *testing.T) {
	score := func(q, target string) int {
		t.Helper()
		s, _, ok := Score(q, target)
		if !ok {
			t.Fatalf("Score(%q, %q) did not match", q, target)
		}
		return s
	}
	tests := []struct {
		name   string
		better [2]string // query and target that must score higher
		worse  [2]string
	}{
		{"consecutive beats sparse", [2]string{"ab", "xabc"}, [2]string{"ab", "xaxb"}},
		{"word boundary beats mid-word", [2]string{"sf", "Save File"}, [2]string{"af", "Save File"}},
		{"start of string beats mid-word", [2]string{"s", "Save File"}, [2]string{"a", "Save File"}},
		{"smaller gap beats larger gap", [2]string{"ab", "axb"}, [2]string{"ab", "axxxxb"}},
		{"exact name beats longer name", [2]string{"main.go", "main.go"}, [2]string{"main.go", "main.go.bak"}},
		{"camel case beats flat", [2]string{"sf", "saveFile"}, [2]string{"sf", "savefile"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			better := score(tt.better[0], tt.better[1])
			worse := score(tt.worse[0], tt.worse[1])
			if better <= worse {
				t.Errorf("%q on %q = %d should beat %q on %q = %d",
					tt.better[0], tt.better[1], better, tt.worse[0], tt.worse[1], worse)
			}
		})
	}
}

func TestScoreOrderingFromFresh(t *testing.T) {
	// The orderings below are the ones asserted by Fresh's fuzzy/mod.rs tests.
	score := func(q, target string) int {
		r := fuzzyMatch(q, target)
		if !r.matched {
			t.Fatalf("fuzzyMatch(%q, %q) did not match", q, target)
		}
		return r.score
	}
	if a, b := score("ab", "xabc"), score("ab", "xaxb"); a <= b {
		t.Errorf("consecutive %d should beat sparse %d", a, b)
	}
	if a, b := score("sf", "Save File"), score("af", "Save File"); a <= b {
		t.Errorf("word boundary %d should beat mid-word %d", a, b)
	}
	if a, b := score("s", "Save File"), score("a", "Save File"); a <= b {
		t.Errorf("start of string %d should beat mid-word %d", a, b)
	}
}

func TestScoreMatchPositions(t *testing.T) {
	_, indexes, ok := Score("sf", "Save File")
	if !ok || !slices.Equal(indexes, []int{0, 5}) {
		t.Fatalf("positions = %v, %v; want [0 5]", indexes, ok)
	}
}

func TestScoreBasenameAndSegmentBonuses(t *testing.T) {
	// A contiguous match starting a basename beats one starting an interior
	// segment, which beats one in the middle of a segment.
	basename := fuzzyMatch("main", "src/main.go").score
	segment := fuzzyMatch("main", "main/src.go").score
	middle := fuzzyMatch("main", "src/xmain.go").score
	if basename <= segment || segment <= middle {
		t.Errorf("basename %d, segment %d, middle %d; want decreasing", basename, segment, middle)
	}
	// Basename prefix followed by a separator earns the exact-basename bonus.
	exact := fuzzyMatch("main", "main-app.go").score
	if exact <= basename {
		t.Errorf("exact basename prefix %d should beat plain basename %d", exact, basename)
	}
}

func TestScoreMultiTermSpan(t *testing.T) {
	tight, _, tightOK := Score("save file", "save_file.rs")
	loose, _, looseOK := Score("save file", "save the whole thing in file.rs")
	if !tightOK || !looseOK {
		t.Fatalf("both should match: tight=%v loose=%v", tightOK, looseOK)
	}
	if tight <= loose {
		t.Errorf("tight span %d should beat loose %d", tight, loose)
	}
	if _, _, ok := Score("file save", "save file"); !ok {
		t.Error("terms may appear in any order")
	}
	if _, _, ok := Score("one two", "one"); ok {
		t.Error("every term must match")
	}
}

func TestScoreCamelCaseBonus(t *testing.T) {
	camel := fuzzyMatch("sf", "saveFile").score
	flat := fuzzyMatch("sf", "savefile").score
	if camel <= flat {
		t.Errorf("camelCase boundary %d should beat flat %d", camel, flat)
	}
}

func TestPatternMatchesScore(t *testing.T) {
	targets := []string{"main.go", "internal/palette/fuzzy.go", "Add Cursor Below", "", "README", "café"}
	queries := []string{"", "go", "main.go", "palette fuzzy", "CURSOR below", "zzz", "  ", "é"}
	for _, q := range queries {
		p := NewPattern(q)
		for _, target := range targets {
			score, idx, ok := p.Match(target)
			wantScore, wantIdx, wantOK := Score(q, target)
			if ok != wantOK || score != wantScore || !slices.Equal(idx, wantIdx) {
				t.Errorf("Pattern(%q).Match(%q) = %d, %v, %v; Score gives %d, %v, %v",
					q, target, score, idx, ok, wantScore, wantIdx, wantOK)
			}
		}
	}
}

func TestPatternRequiresEveryTerm(t *testing.T) {
	p := NewPattern("fuzzy zzz")
	if _, _, ok := p.Match("internal/palette/main.go"); ok {
		t.Fatal("a term with no match still matched")
	}
	if score, pos, ok := NewPattern("").Match("anything"); !ok || score != 0 || pos != nil {
		t.Fatalf("empty pattern = %d, %v, %v; want 0, nil, true", score, pos, ok)
	}
}
