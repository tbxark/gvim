package lsphost

import (
	"context"
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

// TestCapWorkspaceDiagnostics checks the per-file bound: exact duplicates are
// removed, the diagnostics are ordered most severe first, and a noisy file keeps
// only the first workspaceDiagPerFileCap.
func TestCapWorkspaceDiagnostics(t *testing.T) {
	t.Parallel()
	rng := func(line uint32) protocol.Range {
		return protocol.Range{Start: protocol.Position{Line: line}, End: protocol.Position{Line: line, Character: 3}}
	}
	dup := protocol.Diagnostic{Range: rng(1), Severity: protocol.DiagnosticSeverityWarning, Message: protocol.String("same")}
	items := []protocol.Diagnostic{
		dup, dup,
		{Range: rng(2), Severity: protocol.DiagnosticSeverityError, Message: protocol.String("bad")},
		{Range: rng(0), Severity: protocol.DiagnosticSeverityInformation, Message: protocol.String("info")},
	}
	got := capWorkspaceDiagnostics(items)
	if len(got) != 3 {
		t.Fatalf("items = %+v, want the duplicate removed", got)
	}
	if got[0].Severity != protocol.DiagnosticSeverityError || got[1].Severity != protocol.DiagnosticSeverityWarning {
		t.Fatalf("order = %v, want most severe first", got)
	}
	// The per-file cap keeps the most severe first.
	big := make([]protocol.Diagnostic, 0, workspaceDiagPerFileCap+10)
	for i := range workspaceDiagPerFileCap + 10 {
		big = append(big, protocol.Diagnostic{
			Range: rng(uint32(i)), Severity: protocol.DiagnosticSeverityHint, Message: protocol.String("hint"),
		})
	}
	big[workspaceDiagPerFileCap+5].Severity = protocol.DiagnosticSeverityError
	capped := capWorkspaceDiagnostics(big)
	if len(capped) != workspaceDiagPerFileCap {
		t.Fatalf("capped = %d items, want %d", len(capped), workspaceDiagPerFileCap)
	}
	if capped[0].Severity != protocol.DiagnosticSeverityError {
		t.Fatal("the cap dropped the most severe diagnostic")
	}
}

// TestMergeWorkspaceDiagnosticsPrecedence checks the store merge: a file the
// servers published for keeps its published set (and is not reported twice), and a
// workspace-only file is added with the workspace source.
func TestMergeWorkspaceDiagnosticsPrecedence(t *testing.T) {
	t.Parallel()
	published := map[string][]protocol.Diagnostic{
		"/p/a.go": {{Severity: protocol.DiagnosticSeverityError, Message: protocol.String("published")}},
	}
	pulled := map[string][]protocol.Diagnostic{
		"/p/a.go": {{Severity: protocol.DiagnosticSeverityError, Message: protocol.String("pulled")}},
		"/p/b.go": {{Severity: protocol.DiagnosticSeverityWarning, Message: protocol.String("pulled only")}},
	}
	merged, source := mergeWorkspaceDiagnostics(published, pulled)
	if len(merged) != 2 {
		t.Fatalf("merged = %d files, want 2", len(merged))
	}
	if got := diagnosticText(merged["/p/a.go"][0].Message); got != "published" {
		t.Fatalf("a.go = %q, want the published set", got)
	}
	if source["/p/a.go"] != "publish" || source["/p/b.go"] != "workspace" {
		t.Fatalf("sources = %v", source)
	}
}

// TestInlayHintsCarryPlacementPartsAndPadding checks the data the tier-2 placement
// needs: the UTF-16 character becomes a rune column of its line (clamped to it), the
// label parts are kept, the padding flags are carried, and hints outside the document
// or with an empty label are dropped.
func TestInlayHintsCarryPlacementPartsAndPadding(t *testing.T) {
	t.Parallel()
	lines := newTextLines("héllo\nx🙂y\nlast")
	yes, no := true, false
	hints := []protocol.InlayHint{
		{Position: protocol.Position{Line: 0, Character: 2}, Label: protocol.String(": int")},
		{Position: protocol.Position{Line: 1, Character: 1}, Kind: protocol.InlayHintKindParameter,
			Label:       protocol.InlayHintLabelPartSlice{{Value: "a"}, {Value: ": "}, {Value: "int"}},
			PaddingLeft: &yes, PaddingRight: &no},
		{Position: protocol.Position{Line: 1, Character: 3}, Label: protocol.String("after emoji")},
		{Position: protocol.Position{Line: 2, Character: 99}, Label: protocol.String("end")},
		{Position: protocol.Position{Line: 5, Character: 0}, Label: protocol.String("gone")},
		{Position: protocol.Position{Line: 0, Character: 0}, Label: protocol.String("   ")},
	}
	got := inlayHintsFrom(lines, hints)
	if len(got) != 4 {
		t.Fatalf("hints = %+v, want 4 (an out-of-document line and an empty label dropped)", got)
	}
	// "héllo": two UTF-16 units before the second l; rune column 2.
	if got[0].Line != 0 || got[0].Col != 2 || got[0].Text != ": int" {
		t.Fatalf("string hint = %+v, want line 0 col 2", got[0])
	}
	if !slices.Equal(got[0].Parts, []string{": int"}) {
		t.Fatalf("string hint parts = %q", got[0].Parts)
	}
	// "x🙂y": the emoji is two units; character 1 is the rune column after x.
	second := got[1]
	if second.Line != 1 || second.Col != 1 || second.Text != "a: int" ||
		!slices.Equal(second.Parts, []string{"a", ": ", "int"}) {
		t.Fatalf("parts hint = %+v", second)
	}
	if !second.PaddingLeft || second.PaddingRight || second.Kind != 2 {
		t.Fatalf("padding/kind = %+v, want left only and parameter kind", second)
	}
	// Character 3 is past the two-unit emoji: rune column 2.
	if got[2].Line != 1 || got[2].Col != 2 {
		t.Fatalf("emoji hint = %+v, want line 1 col 2", got[2])
	}
	// A column past the line clamps to its length.
	if got[3].Line != 2 || got[3].Col != 4 {
		t.Fatalf("end hint = %+v, want line 2 col 4 (clamped)", got[3])
	}
}

// TestPullSetCancelsSupersededAndAllPulls checks the in-flight workspace pulls: a new pull
// cancels the ones already running, a returned pull is forgotten, and stopping the servers
// cancels everything.
func TestPullSetCancelsSupersededAndAllPulls(t *testing.T) {
	var s pullSet
	var calls []string
	newCancel := func(name string) context.CancelFunc {
		return func() { calls = append(calls, name) }
	}

	first := s.begin(newCancel("first"))
	if len(calls) != 0 {
		t.Fatalf("begin of the first pull cancelled %v", calls)
	}
	second := s.begin(newCancel("second"))
	_ = second
	if !slices.Equal(calls, []string{"first"}) {
		t.Fatalf("begin of a newer pull cancelled %v, want the first", calls)
	}
	// The superseded pull returning is a no-op: its id is already gone.
	s.end(first)
	if len(calls) != 1 {
		t.Fatalf("end of a superseded pull cancelled %v", calls)
	}
	s.cancelAll()
	if !slices.Equal(calls, []string{"first", "second"}) {
		t.Fatalf("cancelAll cancelled %v", calls)
	}
	// The set is ready for the next pull after a cancel.
	third := s.begin(newCancel("third"))
	s.end(third)
	if len(calls) != 2 {
		t.Fatalf("a pull after cancelAll cancelled %v", calls)
	}
}
