package editor

import (
	"strings"
	"testing"
)

// wordSnapshot builds a snapshot with the caret at the end of the last line.
func wordSnapshot(prefix string, lines ...string) WordSnapshot {
	return WordSnapshot{
		Lines: lines, StartLine: 0, CursorLine: len(lines) - 1,
		CursorCol: len([]rune(lines[len(lines)-1])), Prefix: prefix,
		PrefixHasUpper: HasUpper(prefix),
		ViewportTop:    0, ViewportBottom: len(lines) - 1,
	}
}

func candidateTexts(candidates []WordCandidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Text
	}
	return out
}

func TestCompletionPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line, extra string
		col         int
		want        string
	}{
		{"let result = fo", "", 15, "fo"},
		{"foo_bar + ", "", 7, "foo_bar"},
		{"123", "", 3, "123"},
		{"", "", 0, ""},
		{"abc", "", 99, "abc"},
		{"clojure-key", "-", 11, "clojure-key"},
		{"x", "-", -1, ""},
	}
	for _, tt := range tests {
		if got := CompletionPrefix(tt.line, tt.col, tt.extra); got != tt.want {
			t.Fatalf("CompletionPrefix(%q, %d, %q) = %q, want %q", tt.line, tt.col, tt.extra, got, tt.want)
		}
	}
}

func TestBufferWordCompletionsNearestAndFrequency(t *testing.T) {
	t.Parallel()
	lines := []string{
		"func apple() {}",
		"filler filler filler",
		"appetite",
		"app",
	}
	snap := wordSnapshot("app", lines...)
	got := candidateTexts(BufferWordCompletions(snap))
	if len(got) != 2 || got[0] != "appetite" || got[1] != "apple" {
		t.Fatalf("candidates = %v, want nearest appetite then apple", got)
	}
	// The word being typed is not a candidate.
	for _, c := range got {
		if c == "app" {
			t.Fatalf("the typed prefix was offered: %v", got)
		}
	}
}

func TestBufferWordCompletionsSmartCase(t *testing.T) {
	t.Parallel()
	lines := []string{"Test TestOne testText tests", "Te"}
	// An uppercase prefix matches case-sensitively.
	got := candidateTexts(BufferWordCompletions(wordSnapshot("Te", lines...)))
	for _, c := range got {
		if strings.HasPrefix(c, "te") {
			t.Fatalf("lowercase word %q offered for an uppercase prefix: %v", c, got)
		}
	}
	if len(got) == 0 || got[0] != "Test" {
		t.Fatalf("candidates = %v, want Test first", got)
	}
	// A lowercase prefix matches both casings; the exact casing scores higher.
	lower := BufferWordCompletions(WordSnapshot{
		Lines: []string{"Test testText tests", "te"}, StartLine: 0, CursorLine: 1,
		CursorCol: 2, Prefix: "te", ViewportTop: 0, ViewportBottom: 1,
	})
	texts := candidateTexts(lower)
	if len(texts) < 2 || texts[0] != "testText" {
		t.Fatalf("lowercase candidates = %v, want testText first", texts)
	}
}

func TestBufferWordCompletionsMinLengthAndLimit(t *testing.T) {
	t.Parallel()
	// One-rune words never match because the prefix is at least one rune and a
	// candidate must be at least two.
	if got := BufferWordCompletions(wordSnapshot("x", "x y z")); len(got) != 0 {
		t.Fatalf("one-rune words offered: %v", candidateTexts(got))
	}
	lines := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		lines = append(lines, "alpha"+strings.Repeat("a", i%5)+string(rune('a'+i%26)))
	}
	lines = append(lines, "al")
	got := BufferWordCompletions(wordSnapshot("al", lines...))
	if len(got) != WordCompletionLimit {
		t.Fatalf("candidates = %d, want the cap %d", len(got), WordCompletionLimit)
	}
}

func TestBufferWordCompletionsViewportBoost(t *testing.T) {
	t.Parallel()
	// "seen" appears far away and also on screen; the on-screen occurrence gets
	// the boost, so it outranks an equally sized nearer-in-lines word that is
	// outside the viewport.
	lines := []string{
		"seen seen", // line 0, far from caret
		"middle",
		"middle",
		"middle",
		"middle",
		"visible seen",
		"se",
	}
	snap := WordSnapshot{
		Lines: lines, StartLine: 0, CursorLine: 6, CursorCol: 2, Prefix: "se",
		ViewportTop: 5, ViewportBottom: 6,
	}
	got := BufferWordCompletions(snap)
	if len(got) == 0 {
		t.Fatal("no candidates")
	}
	// seen is visible and frequent, so it must be first.
	if got[0].Text != "seen" {
		t.Fatalf("candidates = %v, want seen boosted first", candidateTexts(got))
	}
}

func TestBufferWordCompletionsExtraWordChars(t *testing.T) {
	t.Parallel()
	snap := WordSnapshot{
		Lines: []string{"foo-bar foo-barish", "foo-b"}, StartLine: 0, CursorLine: 1,
		CursorCol: 5, Prefix: "foo-b", WordChars: "-", ViewportTop: 0, ViewportBottom: 1,
	}
	got := candidateTexts(BufferWordCompletions(snap))
	if len(got) != 2 || got[0] != "foo-barish" || got[1] != "foo-bar" {
		t.Fatalf("candidates = %v, want the hyphenated words nearest first", got)
	}
	// Without the extra character the hyphen breaks the word, so only the tail
	// could match, and it does not start with the prefix.
	plain := snap
	plain.WordChars = ""
	if got := BufferWordCompletions(plain); len(got) != 0 {
		t.Fatalf("candidates without word chars = %v, want none", candidateTexts(got))
	}
}

func TestBufferWordCompletionsUnicode(t *testing.T) {
	t.Parallel()
	snap := WordSnapshot{
		Lines: []string{"héllo héro", "hé"}, StartLine: 0, CursorLine: 1,
		CursorCol: 2, Prefix: "hé", ViewportTop: 0, ViewportBottom: 1,
	}
	got := candidateTexts(BufferWordCompletions(snap))
	if len(got) != 2 || got[0] != "héllo" || got[1] != "héro" {
		t.Fatalf("candidates = %v, want unicode words nearest first", got)
	}
}

func TestBufferWordCompletionsEmptyPrefix(t *testing.T) {
	t.Parallel()
	if got := BufferWordCompletions(wordSnapshot("", "alpha beta")); got != nil {
		t.Fatalf("empty prefix returned %v", got)
	}
}
