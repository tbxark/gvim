package fuzzy

import (
	"slices"
	"testing"
)

// TestMatcherMatchesPattern pins the reusable matcher to the Pattern/Score results,
// including multi-term queries and legal paths containing spaces.
func TestMatcherMatchesPattern(t *testing.T) {
	targets := []string{
		"features/groups/view.tsx",
		"/etc/hosts",
		"save_file.rs",
		"my file.txt",
		"src/main.go",
		"internal/ui/palette/palette.go",
		"café/notes.txt",
		"[No Name]",
		"Save File.rs",
	}
	queries := []string{"", "file", "save file", "file save", "etc hosts", "my file", "features groups", "café", "zzz"}
	for _, q := range queries {
		m := NewMatcher(q)
		pat := NewPattern(q)
		for _, target := range targets {
			gotScore, gotIdx, gotOK := m.Match(target)
			wantScore, wantIdx, wantOK := pat.Match(target)
			if gotOK != wantOK || gotScore != wantScore || !slices.Equal(gotIdx, wantIdx) {
				t.Errorf("Matcher(%q).Match(%q) = %d, %v, %v; Pattern gives %d, %v, %v",
					q, target, gotScore, gotIdx, gotOK, wantScore, wantIdx, wantOK)
			}
			scoreOnly, okOnly := m.MatchScore(target)
			if okOnly != wantOK || scoreOnly != wantScore {
				t.Errorf("Matcher(%q).MatchScore(%q) = %d, %v; want %d, %v",
					q, target, scoreOnly, okOnly, wantScore, wantOK)
			}
			// The same matcher is reused across targets and calls.
			againScore, againIdx, againOK := m.Match(target)
			if againOK != wantOK || againScore != wantScore || !slices.Equal(againIdx, wantIdx) {
				t.Errorf("Matcher reused: %q/%q = %d, %v, %v; want %d, %v, %v",
					q, target, againScore, againIdx, againOK, wantScore, wantIdx, wantOK)
			}
		}
	}
	var nilM *Matcher
	if s, ok := nilM.MatchScore("abc"); s != 0 || ok {
		t.Fatalf("nil Matcher.MatchScore = %d, %v; want 0, false", s, ok)
	}
}

// TestMatcherASCIIRejectDoesNotAllocate: the fast rejection must not convert a
// non-matching ASCII target to runes for every term.
func TestMatcherASCIIRejectDoesNotAllocate(t *testing.T) {
	m := NewMatcher("zzz term")
	allocs := testing.AllocsPerRun(100, func() {
		if _, _, ok := m.Match("src/main/regular_file.go"); ok {
			t.Fatal("a non-matching target matched")
		}
	})
	if allocs != 0 {
		t.Fatalf("ASCII rejection allocated %.1f objects per call, want 0", allocs)
	}
}

func TestMatcherMatchScoreZeroAllocAfterWarmup(t *testing.T) {
	for _, q := range []string{"edhan", "pane handler"} {
		m := NewMatcher(q)
		target := "internal/tui/editorpane/component_0001/handler.go"
		// Warm up scratch buffers to target size.
		if _, ok := m.MatchScore(target); !ok {
			t.Fatalf("expected %q to match %q", q, target)
		}
		allocs := testing.AllocsPerRun(100, func() {
			if _, ok := m.MatchScore(target); !ok {
				t.Fatalf("expected %q to match %q", q, target)
			}
		})
		if allocs != 0 {
			t.Fatalf("MatchScore(%q) allocated %.1f objects per hit after warmup, want 0", q, allocs)
		}
	}
}
