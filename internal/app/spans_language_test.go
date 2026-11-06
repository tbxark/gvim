package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/syntax"
)

// newSpanAdapter opens path in a new editor and returns its adapter with an empty span
// cache, as newUIDeps builds it.
func newSpanAdapter(t *testing.T, path, text string) editorAdapter {
	t.Helper()
	must(t, os.WriteFile(path, []byte(text), 0o644))
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(path))
	return editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
}

// kindAt returns the kind of the span that covers byte offset col of the line spans,
// or KindPlain when no span covers it.
func kindAt(spans []syntax.Span, col int) syntax.Kind {
	for _, s := range spans {
		if s.Start <= col && col < s.End {
			return s.Kind
		}
	}
	return syntax.KindPlain
}

// TestSpansUseTheSetLanguageOrTheFileName checks which lexer colors the first word of a
// document, which is "package" in every case. A set language names the lexer. With no
// language, or one that names no lexer, the file name chooses it.
func TestSpansUseTheSetLanguageOrTheFileName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		file     string // file name, in a temporary folder
		lang     string // language set with set_language before the spans are read, or ""
		wantKind syntax.Kind
	}{
		{
			name:     "a set language highlights a file whose name has no lexer",
			file:     "notes.txt",
			lang:     "go",
			wantKind: syntax.KindKeyword,
		},
		{
			name:     "with no language the file name chooses the lexer",
			file:     "main.go",
			wantKind: syntax.KindKeyword,
		},
		{
			name:     "with no language and no lexer for the name there is no highlight",
			file:     "notes.txt",
			wantKind: syntax.KindPlain,
		},
		{
			name:     "a language that names no lexer falls back to the file name",
			file:     "main.go",
			lang:     "zzqx",
			wantKind: syntax.KindKeyword,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := newSpanAdapter(t, filepath.Join(t.TempDir(), tt.file), "package main\n")
			if tt.lang != "" {
				if r := a.Do("set_language", map[string]any{"name": tt.lang}); r.Err != nil {
					t.Fatalf("set_language: %v", r.Err)
				}
			}
			spans := a.Active().LineSpans(0)
			if got := kindAt(spans, 0); got != tt.wantKind {
				t.Fatalf("kind of %q = %v, want %v (spans %v)", "package", got, tt.wantKind, spans)
			}
		})
	}
}

// TestSetLanguageRelexesAnOpenTab checks that a language set on the active tab changes its
// spans at once. The tab was lexed with no language first, so its cache holds the file
// name's lexer, and that cache must be replaced.
func TestSetLanguageRelexesAnOpenTab(t *testing.T) {
	t.Parallel()
	a := newSpanAdapter(t, filepath.Join(t.TempDir(), "notes.txt"), "package main\n")
	if got := kindAt(a.Active().LineSpans(0), 0); got != syntax.KindPlain {
		t.Fatalf("before set_language kind = %v, want plain", got)
	}
	if r := a.Do("set_language", map[string]any{"name": "go"}); r.Err != nil {
		t.Fatalf("set_language: %v", r.Err)
	}
	if got := kindAt(a.Active().LineSpans(0), 0); got != syntax.KindKeyword {
		t.Fatalf("after set_language kind = %v, want keyword", got)
	}
}
