package plugin

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// src builds a DiffSource with the editor's byte model: one byte per line break.
func src(lines ...string) pluginapi.DiffSource {
	s := pluginapi.DiffSource{Lines: lines, Starts: make([]int, len(lines)), LineEnding: "LF"}
	off := 0
	for i, line := range lines {
		s.Starts[i] = off
		off += len(line)
		if i < len(lines)-1 {
			off++
		}
	}
	s.Len = off
	return s
}

// TestCompareSideGeometry checks the byte geometry of one hunk side: line starts,
// the replaced range including the last line break, and the jump for an empty side.
func TestCompareSideGeometry(t *testing.T) {
	s := src("aa", "bbb", "c")
	tests := []struct {
		name  string
		start int
		count int
		want  pluginapi.CompareSide
	}{
		{"one line", 1, 1, pluginapi.CompareSide{
			Start: 1, Count: 1, From: 3, To: 7, Jump: 3, Lines: [][2]int{{3, 6}},
		}},
		{"to the end", 1, 2, pluginapi.CompareSide{
			Start: 1, Count: 2, From: 3, To: 8, Jump: 3, Lines: [][2]int{{3, 6}, {7, 8}},
		}},
		{"empty at top", 0, 0, pluginapi.CompareSide{Start: 0, Count: 0, From: 0, To: 0, Jump: 0}},
		{"empty in the middle jumps to the line before", 2, 0, pluginapi.CompareSide{
			Start: 2, Count: 0, From: 7, To: 7, Jump: 3,
		}},
		{"empty at the end", 3, 0, pluginapi.CompareSide{Start: 3, Count: 0, From: 8, To: 8, Jump: 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareSide(s, tt.start, tt.count)
			if got.Start != tt.want.Start || got.Count != tt.want.Count ||
				got.From != tt.want.From || got.To != tt.want.To || got.Jump != tt.want.Jump {
				t.Fatalf("side = %+v, want %+v", got, tt.want)
			}
			if len(got.Lines) != len(tt.want.Lines) {
				t.Fatalf("lines = %v, want %v", got.Lines, tt.want.Lines)
			}
			for i := range got.Lines {
				if got.Lines[i] != tt.want.Lines[i] {
					t.Fatalf("line %d = %v, want %v", i, got.Lines[i], tt.want.Lines[i])
				}
			}
		})
	}
}

// TestBuildCompareResult checks the assembled result: hunk sides, byte ranges, line
// counts, byte lengths and versions, for insert, delete and change hunks.
func TestBuildCompareResult(t *testing.T) {
	left := src("a", "b", "c")
	left.Version = pluginapi.Version{Gen: 1, Ver: 2}
	right := src("a", "x", "c", "d")
	right.Version = pluginapi.Version{Gen: 2, Ver: 5}
	hunks := []pluginapi.Hunk{
		{Kind: "change", OldStart: 1, OldCount: 1, NewStart: 1, NewCount: 1},
		{Kind: "insert", OldStart: 3, OldCount: 0, NewStart: 3, NewCount: 1},
	}
	res := buildCompareResult(left, right, hunks)
	if res.LeftLines != 3 || res.RightLines != 4 || res.LeftLen != 5 || res.RightLen != 7 {
		t.Fatalf("counts = %+v", res)
	}
	if res.LeftVersion != left.Version || res.RightVersion != right.Version {
		t.Fatalf("versions = %v/%v", res.LeftVersion, res.RightVersion)
	}
	if len(res.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(res.Hunks))
	}
	change := res.Hunks[0]
	if change.Left.From != 2 || change.Left.To != 4 || change.Right.From != 2 || change.Right.To != 4 {
		t.Fatalf("change ranges = %+v / %+v", change.Left, change.Right)
	}
	// The changed bytes are the b in "b" against the x in "x": the whole line.
	if len(change.Left.Spans) != 1 || !change.Left.Spans[0].Set ||
		change.Left.Spans[0].Start != 2 || change.Left.Spans[0].End != 3 {
		t.Fatalf("left spans = %+v, want the whole line", change.Left.Spans)
	}
	if len(change.Right.Spans) != 1 || !change.Right.Spans[0].Set ||
		change.Right.Spans[0].Start != 2 || change.Right.Spans[0].End != 3 {
		t.Fatalf("right spans = %+v, want the whole line", change.Right.Spans)
	}
	insert := res.Hunks[1]
	if insert.Left.Count != 0 || insert.Right.Count != 1 || insert.Left.Jump != 4 {
		t.Fatalf("insert sides = %+v / %+v", insert.Left, insert.Right)
	}
	if len(insert.Left.Spans) != 0 || len(insert.Right.Spans) != 0 {
		t.Fatalf("insert spans = %v / %v, want none", insert.Left.Spans, insert.Right.Spans)
	}
	if res.Hunks[0].EOLOnly || res.Hunks[1].EOLOnly {
		t.Fatal("gvim normalises line endings; eol_only must stay false")
	}
}

// TestCompareSpansAreUTF8Safe checks the span of a pair that differs in the middle
// of a multi-byte character: the span never cuts a rune.
func TestCompareSpansAreUTF8Safe(t *testing.T) {
	left := src("héllo", "x")
	right := src("hôllo", "x")
	res := buildCompareResult(left, right, []pluginapi.Hunk{
		{Kind: "change", OldStart: 0, OldCount: 1, NewStart: 0, NewCount: 1},
	})
	h := res.Hunks[0]
	if len(h.Left.Spans) != 1 || !h.Left.Spans[0].Set {
		t.Fatalf("left spans = %+v", h.Left.Spans)
	}
	span := h.Left.Spans[0]
	if span.Start != 1 || span.End != 3 {
		t.Fatalf("left span = %+v, want the whole é (bytes 1..3)", span)
	}
	want := h.Right.Spans[0]
	if want.Start != 1 || want.End != 3 {
		t.Fatalf("right span = %+v, want the whole ô (bytes 1..3)", want)
	}
}

// TestCompareSpansCommonPrefix checks that unchanged leading and trailing text is
// left out of the span.
func TestCompareSpansCommonPrefix(t *testing.T) {
	left := src("abcDEFghi")
	right := src("abcXYZghi")
	res := buildCompareResult(left, right, []pluginapi.Hunk{
		{Kind: "change", OldStart: 0, OldCount: 1, NewStart: 0, NewCount: 1},
	})
	span := res.Hunks[0].Left.Spans[0]
	if !span.Set || span.Start != 3 || span.End != 6 {
		t.Fatalf("span = %+v, want 3..6", span)
	}
}

// TestCompareEqualBuffers checks the empty result of equal texts.
func TestCompareEqualBuffers(t *testing.T) {
	res := buildCompareResult(src("a", "b"), src("a", "b"), nil)
	if len(res.Hunks) != 0 {
		t.Fatalf("hunks = %+v, want none", res.Hunks)
	}
	if res.LeftLines != 2 || res.RightLines != 2 {
		t.Fatalf("result = %+v", res)
	}
}
