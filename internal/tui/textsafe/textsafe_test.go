package textsafe

import (
	"testing"
)

// TestClean removes terminal/visual spoof bytes while keeping readable text and the
// tab/newline semantics the preview surfaces paint.
func TestClean(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"plain", "hello world", "hello world"},
		{"tab and newline kept", "a\tb\nc", "a\tb\nc"},
		{"CJK and emoji kept", "你好🙂", "你好🙂"},
		{"C1 controls removed", "a\u0080\u009bb", "ab"},
		{"C0/ANSI removed", "\x1b[31mred\x07", "[31mred"},
		{"bidi overrides removed", "a\u202Eb\u202Dc", "abc"},
		{"zero-width and BOM removed", "\ufeffa\u200Bb\u200Dc\ufeff", "abc"},
		{"soft hyphen removed", "co\u00adop", "coop"},
		{"invalid UTF-8 removed", "a\xffb\xc3", "ab"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clean(tt.in); got != tt.want {
				t.Fatalf("Clean(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCleanWithMap checks the raw-rune map a highlight remap needs, including a leading
// removed rune.
func TestCleanWithMap(t *testing.T) {
	clean, mapping := CleanWithMap("\u202Ea\u200Bb")
	if clean != "ab" {
		t.Fatalf("clean = %q, want ab", clean)
	}
	// raw positions: 0=bidi, 1=a, 2=zwsp, 3=b, sentinel 4.
	for raw, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 2} {
		if got := MapIndex(mapping, raw); got != want {
			t.Errorf("MapIndex(%d) = %d, want %d (mapping %v)", raw, got, want, mapping)
		}
	}
}
