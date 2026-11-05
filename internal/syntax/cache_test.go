package syntax

import (
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
)

// TestHighlighterRevisionHitAndMiss is the spec's cache check: the same
// revision is a hit, and a new revision re-lexes the whole buffer.
func TestHighlighterRevisionHitAndMiss(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("python"), calls: &calls})
	text := []string{"def f():", "    return 1"}

	h.Lines(7, text)
	h.Lines(7, text)
	if calls != 1 {
		t.Fatalf("same revision: lexed %d times, want 1 (hit)", calls)
	}
	edited := []string{"def f():", "    return 2"}
	h.Lines(8, edited)
	if calls != 2 {
		t.Fatalf("new revision: lexed %d times, want 2 (miss)", calls)
	}
}

// bigLines returns n lines of size bytes each.
func bigLines(n, size int) []string {
	line := strings.Repeat("x", size)
	out := make([]string, n)
	for i := range out {
		out[i] = line
	}
	return out
}

func TestHighlighterSizeGuardSkipsLexing(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("go"), calls: &calls})
	text := bigLines(3, 1<<20) // 3 MiB, above MaxHighlightBytes
	spans := h.Lines(1, text)
	if calls != 0 {
		t.Fatalf("oversize buffer was lexed %d times", calls)
	}
	if !h.Skipped() {
		t.Fatal("Skipped should report the size guard")
	}
	reconstruct(t, text, spans)
	for i, line := range spans {
		if len(line) != 1 || line[0].Kind != KindPlain {
			t.Fatalf("line %d: want one plain span, got %+v", i, line)
		}
	}
}

func TestHighlighterAtLimitIsLexed(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("go"), calls: &calls})
	// Two lines of 1 MiB - 1 bytes plus one newline each make 2 MiB exactly.
	text := bigLines(2, MaxHighlightBytes/2-1)
	h.Lines(1, text)
	if h.Skipped() {
		t.Fatal("a buffer at the limit should be lexed")
	}
	if calls != 1 {
		t.Fatalf("buffer at the limit lexed %d times, want 1", calls)
	}
}

func TestHighlighterSkippedClearsWhenSmall(t *testing.T) {
	h := NewHighlighter(lexers.Get("go"))
	h.Lines(1, bigLines(3, 1<<20))
	if !h.Skipped() {
		t.Fatal("expected skip for large buffer")
	}
	h.Lines(2, []string{"package main"})
	if h.Skipped() {
		t.Fatal("small buffer should clear Skipped")
	}
}

// TestDetectUntitledShebangIsShell checks the spec's untitled-buffer case: no
// file name, so the shebang and Analyse pick the shell lexer.
func TestDetectUntitledShebangIsShell(t *testing.T) {
	for _, src := range []string{
		"#!/bin/sh\necho hi\n",
		"#!/usr/bin/env bash\necho hi\n",
	} {
		got := Detect("", []byte(src)).Config().Name
		if got != "Bash" {
			t.Errorf("Detect(untitled %q) = %q, want Bash", src[:12], got)
		}
	}
}

func BenchmarkTokenizeGo(b *testing.B) {
	unit := "func add(x int, y int) int { return x + y*42 } // sum of two ints\n"
	text := strings.Split(strings.Repeat(unit, (2<<20)/len(unit)), "\n")
	lexer := lexers.Get("go")
	b.SetBytes(int64(size(text)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := Tokenize(lexer, text); err != nil {
			b.Fatal(err)
		}
	}
}
