package syntax

import "testing"

func TestMarkdownFencesClassifies(t *testing.T) {
	t.Parallel()
	lines := []string{
		"# Title",
		"```go",
		"package main",
		"",
		"```",
		"text",
		"  ~~~~ python",
		"print(1)",
		"~~~~",
		"\u0060\u0060\u0060 unclosed",
		"body",
	}
	got := MarkdownFences(lines)
	tests := []struct {
		line int
		kind FenceKind
		info string
	}{
		{0, FenceNone, ""},
		{1, FenceOpen, "go"},
		{2, FenceBody, ""},
		{3, FenceBody, ""},
		{4, FenceClose, ""},
		{5, FenceNone, ""},
		{6, FenceOpen, "python"},
		{7, FenceBody, ""},
		{8, FenceClose, ""},
		{9, FenceOpen, "unclosed"},
		{10, FenceBody, ""},
	}
	for _, tt := range tests {
		if got[tt.line].Kind != tt.kind {
			t.Fatalf("line %d = %v, want %v", tt.line, got[tt.line].Kind, tt.kind)
		}
		if tt.info != "" && got[tt.line].Info != tt.info {
			t.Fatalf("line %d info = %q, want %q", tt.line, got[tt.line].Info, tt.info)
		}
	}
	if got[6].Indent != 2 || got[1].Marker != "```" {
		t.Fatalf("classification = %+v", got[6])
	}
}
