package lsphost

import (
	"slices"
	"testing"

	"go.lsp.dev/protocol"

	"github.com/tbxark/gvim/internal/tui"
)

// TestDecodeSemanticTokens checks the protocol quintuples decode to rune-column
// spans with the legend's type and modifier names, and that only the requested
// lines come back.
func TestDecodeSemanticTokens(t *testing.T) {
	t.Parallel()
	legend := &protocol.SemanticTokensLegend{
		TokenTypes:     []string{"keyword", "function", "variable"},
		TokenModifiers: []string{"declaration", "readonly", "async"},
	}
	lines := newTextLines("func main\n    return x")
	// Line 0: "func" keyword (chars 0-3), "main" function (chars 5-9).
	// Line 1: "return" keyword (chars 0-5), "x" variable (chars 9-10), readonly.
	data := []uint32{
		0, 0, 4, 0, 0,
		0, 5, 4, 1, 0,
		1, 0, 6, 0, 0,
		0, 9, 1, 2, 1<<0 | 1<<1,
	}
	all := decodeSemanticTokens(data, legend, lines, 0, 1)
	want := []tui.SemanticToken{
		{Line: 0, Start: 0, End: 4, Type: "keyword"},
		{Line: 0, Start: 5, End: 9, Type: "function"},
		{Line: 1, Start: 0, End: 6, Type: "keyword"},
		{Line: 1, Start: 9, End: 10, Type: "variable", Modifiers: []string{"declaration", "readonly"}},
	}
	if !slices.EqualFunc(all, want, semanticTokenEqual) {
		t.Fatalf("tokens = %+v, want %+v", all, want)
	}
	if got := decodeSemanticTokens(data, legend, lines, 1, 1); len(got) != 2 || got[0].Line != 1 {
		t.Fatalf("visible lines = %+v, want line 1 only", got)
	}
}

// semanticTokenEqual compares two tokens, modifiers included.
func semanticTokenEqual(a, b tui.SemanticToken) bool {
	return a.Line == b.Line && a.Start == b.Start && a.End == b.End &&
		a.Type == b.Type && slices.Equal(a.Modifiers, b.Modifiers)
}

// TestDecodeSemanticTokensCountsUTF16Columns checks the character conversion: the
// protocol counts UTF-16 units, the editor runes.
func TestDecodeSemanticTokensCountsUTF16Columns(t *testing.T) {
	t.Parallel()
	legend := &protocol.SemanticTokensLegend{TokenTypes: []string{"variable"}}
	lines := newTextLines("é日x")
	// "é" and "日" are one UTF-16 unit each; the token covers both (cols 1 and 2).
	data := []uint32{0, 1, 2, 0, 0}
	got := decodeSemanticTokens(data, legend, lines, 0, 0)
	if len(got) != 1 || got[0].Start != 1 || got[0].End != 3 {
		t.Fatalf("tokens = %+v, want rune columns 1..3", got)
	}
}

// TestApplySemanticTokenEdits checks the delta application: an edit cuts a run of
// the old data and inserts the new one, and a later edit sees the shortened data.
func TestApplySemanticTokenEdits(t *testing.T) {
	t.Parallel()
	old := []uint32{0, 0, 3, 1, 0, 0, 4, 2, 2, 0, 1, 0, 1, 3, 0}
	tests := []struct {
		name  string
		edits []protocol.SemanticTokensEdit
		want  []uint32
	}{
		{
			name:  "replace one token",
			edits: []protocol.SemanticTokensEdit{{Start: 5, DeleteCount: 5, Data: []uint32{0, 5, 3, 2, 0}}},
			want:  []uint32{0, 0, 3, 1, 0, 0, 5, 3, 2, 0, 1, 0, 1, 3, 0},
		},
		{
			name:  "delete a token",
			edits: []protocol.SemanticTokensEdit{{Start: 0, DeleteCount: 5}},
			want:  []uint32{0, 4, 2, 2, 0, 1, 0, 1, 3, 0},
		},
		{
			name:  "insert at the end",
			edits: []protocol.SemanticTokensEdit{{Start: 15, DeleteCount: 0, Data: []uint32{1, 0, 2, 1, 0}}},
			want:  []uint32{0, 0, 3, 1, 0, 0, 4, 2, 2, 0, 1, 0, 1, 3, 0, 1, 0, 2, 1, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := applySemanticTokenEdits(old, tt.edits); !slices.Equal(got, tt.want) {
				t.Fatalf("data = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestHintTextFlattensLabels checks the inlay hint label reduction: a string is
// itself and the parts are joined in order.
func TestHintTextFlattensLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		label protocol.InlayHintLabel
		want  string
	}{
		{"a string", protocol.String(": int"), ": int"},
		{"parts", protocol.InlayHintLabelPartSlice{{Value: "x"}, {Value: ": "}, {Value: "int"}}, "x: int"},
		{"empty parts", protocol.InlayHintLabelPartSlice{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hintText(tt.label); got != tt.want {
				t.Fatalf("hintText = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestClampFoldRanges checks the fold-range clamping: ranges past the document are
// dropped, and a range that runs past its end is cut to it, since the editor cannot
// fold lines that are not there.
func TestClampFoldRanges(t *testing.T) {
	t.Parallel()
	ranges := []protocol.FoldingRange{
		{StartLine: 1, EndLine: 4},
		{StartLine: 3, EndLine: 99},
		{StartLine: 9, EndLine: 90}, // starts past the last line: dropped
		{StartLine: 4, EndLine: 2},  // reversed: dropped
	}
	got := clampFoldRanges(ranges, 5)
	want := []tui.FoldRange{{StartLine: 1, EndLine: 4}, {StartLine: 3, EndLine: 5}}
	if !slices.Equal(got, want) {
		t.Fatalf("ranges = %+v, want %+v", got, want)
	}
	if got := clampFoldRanges(ranges, -1); len(got) != 0 {
		t.Fatalf("ranges of an empty document = %+v, want none", got)
	}
}
