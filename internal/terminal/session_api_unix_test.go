//go:build unix

package terminal

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// startWithScrollback starts /bin/sh -c script with a scrollback of lines rows.
func startWithScrollback(t *testing.T, script string, lines int) *Session {
	t.Helper()
	s, err := Start(context.Background(), Options{
		Shell: "/bin/sh", Args: []string{"-c", script}, Cols: 200, Rows: 24, ScrollbackLines: lines,
	})
	if err != nil {
		t.Fatalf("Start(%q): %v", script, err)
	}
	t.Cleanup(func() { closeSession(t, s) })
	return s
}

// moreLines is a shell script that prints n numbered lines and then stays alive.
func moreLines(n int) string {
	return "i=1; while [ $i -le " + itoaTest(n) + " ]; do echo line-$i; i=$((i+1)); done; exec sleep 5"
}

// itoaTest formats n without importing strconv in a script string.
func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func waitScrollback(t *testing.T, s *Session, want func(total int) bool) int {
	t.Helper()
	var total int
	waitFor(t, s, "scrollback rows", func(Screen) bool {
		total = s.ScrollbackLen()
		return want(total)
	})
	return total
}

func TestScrollbackSnapshot(t *testing.T) {
	s := startSh(t, moreLines(60))
	total := waitScrollback(t, s, func(total int) bool { return total >= 30 })
	lines, got := s.Scrollback(0, 5)
	if got != total {
		t.Fatalf("Scrollback total = %d, want %d", got, total)
	}
	if len(lines) != 5 {
		t.Fatalf("lines = %d, want 5", len(lines))
	}
	if got := lines[0].Text(); got != "line-1" {
		t.Fatalf("first history line = %q, want line-1", got)
	}
	// The window starts where it is asked to.
	lines, _ = s.Scrollback(5, 3)
	if len(lines) != 3 || lines[0].Text() != "line-6" {
		t.Fatalf("window at 5 = %q, want line-6", lines[0].Text())
	}
	// The copy is bounded.
	if lines, _ := s.Scrollback(0, MaxScrollbackSnapshot+100); len(lines) > MaxScrollbackSnapshot {
		t.Fatalf("a snapshot copied %d rows, over the bound", len(lines))
	}
	// A window past the end is empty, not an error.
	if lines, _ := s.Scrollback(total+5, 3); len(lines) != 0 {
		t.Fatalf("a window past the end = %d rows, want none", len(lines))
	}
}

func TestScrollbackLinesOptionBoundsTheHistory(t *testing.T) {
	s := startWithScrollback(t, moreLines(40), 5)
	total := waitScrollback(t, s, func(total int) bool { return total == 5 })
	if total != 5 {
		t.Fatalf("history = %d rows, want the configured 5", total)
	}
}

func TestDisabledScrollbackReportsNoHistory(t *testing.T) {
	s := startWithScrollback(t, moreLines(40), -1)
	waitFor(t, s, "the printed lines", func(sc Screen) bool {
		return strings.Contains(screenText(sc), "line-40")
	})
	if got := s.ScrollbackLen(); got != 0 {
		t.Fatalf("ScrollbackLen = %d with the history disabled, want 0", got)
	}
	if lines, total := s.Scrollback(0, 10); total != 0 || len(lines) != 0 {
		t.Fatalf("Scrollback = %d rows/%d total with the history disabled", len(lines), total)
	}
	if matches, _ := s.SearchScrollback("line", 10); len(matches) != 0 {
		t.Fatalf("SearchScrollback found %v in a disabled history", matches)
	}
}

func TestAltScreenHasNoScrollback(t *testing.T) {
	s := startSh(t, "printf '\\033[?1049h'; exec sleep 5")
	waitFor(t, s, "the alternate screen", func(Screen) bool { return s.AltScreen() })
	if got := s.ScrollbackLen(); got != 0 {
		t.Fatalf("scrollback in the alternate screen = %d, want 0", got)
	}
	if lines, total := s.Scrollback(0, 10); total != 0 || len(lines) != 0 {
		t.Fatalf("Scrollback in the alternate screen = %d rows/%d total, want none", len(lines), total)
	}
}

func TestWorkingDirectoryFromOSC7(t *testing.T) {
	s := startSh(t, "printf '\\033]7;file://host/tmp/some dir\\033\\\\'; exec sleep 5")
	waitFor(t, s, "the OSC 7 directory", func(Screen) bool { return s.WorkingDirectory() != "" })
	if got := s.WorkingDirectory(); got != "/tmp/some dir" {
		t.Fatalf("WorkingDirectory = %q, want /tmp/some dir", got)
	}
}

// TestWorkingDirectoryFromOSC7AcceptsWindowsPath checks the OSC 7 flavour follows
// the reporting shell's OS, not gvim's: a Windows drive file URL is kept as a
// Windows path even on this (Unix) host.
func TestWorkingDirectoryFromOSC7AcceptsWindowsPath(t *testing.T) {
	s := startSh(t, "printf '\\033]7;file:///C:/Users/me\\033\\\\'; printf 'mark\\n'; exec sleep 5")
	waitFor(t, s, "the marker", hasText("mark"))
	waitFor(t, s, "the Windows OSC 7 directory", func(Screen) bool { return s.WorkingDirectory() != "" })
	if got := s.WorkingDirectory(); got != "C:/Users/me" {
		t.Fatalf("WorkingDirectory = %q, want the Windows path as reported", got)
	}
}

// TestWorkingDirectoryFromOSC7RejectsTamperedInput checks the child's OSC 7 path is
// untrusted: an over-long payload is ignored instead of being kept and later used
// as a start directory (control bytes are covered by TestOSC7Path; the emulator's
// OSC parser does not hand every control byte through to the callback).
func TestWorkingDirectoryFromOSC7RejectsTamperedInput(t *testing.T) {
	long := startSh(t, "printf '\\033]7;file:///"+
		strings.Repeat("a", maxWorkingDirectoryBytes+10)+
		"\\007'; printf 'mark\\n'; exec sleep 5")
	waitFor(t, long, "the marker", hasText("mark"))
	if got := long.WorkingDirectory(); got != "" {
		t.Fatalf("WorkingDirectory with an over-long path = %q, want none", got)
	}
}

func TestMouseReportingFromChildModes(t *testing.T) {
	s := startSh(t, "printf '\\033[?1000h'; exec sleep 5")
	waitFor(t, s, "mouse reporting", func(Screen) bool {
		reporting, _, _ := s.MouseReporting()
		return reporting
	})
	if _, drag, all := s.MouseReporting(); drag || all {
		t.Fatalf("1000 reported as drag %v all %v, want neither", drag, all)
	}
}

func TestMouseDragReportingFromChildModes(t *testing.T) {
	s := startSh(t, "printf '\033[?1002h'; exec sleep 5")
	waitFor(t, s, "drag reporting", func(Screen) bool {
		_, drag, _ := s.MouseReporting()
		return drag
	})
	if _, _, all := s.MouseReporting(); all {
		t.Fatal("1002 was reported as all-motion")
	}
}

func TestMouseReportingClearsWhenTheChildTurnsItOff(t *testing.T) {
	s := startSh(t, "printf '\\033[?1003h'; sleep 1; printf '\\033[?1003l'; exec sleep 5")
	waitFor(t, s, "all-motion reporting", func(Screen) bool {
		_, _, all := s.MouseReporting()
		return all
	})
	waitFor(t, s, "reporting off", func(Screen) bool {
		reporting, _, _ := s.MouseReporting()
		return !reporting
	})
}

func TestSearchScrollbackMode(t *testing.T) {
	s := startSh(t, moreLines(60))
	total := waitScrollback(t, s, func(total int) bool { return total >= 30 })

	// Case-sensitive literal search finds no uppercase history line.
	if matches, _, err := s.SearchScrollbackMode("LINE-10", false, true, 10); err != nil || len(matches) != 0 {
		t.Fatalf("case-sensitive literal = %v, %v; want no match", matches, err)
	}
	// Case-insensitive literal finds the row.
	matches, _, err := s.SearchScrollbackMode("LINE-10", false, false, 10)
	if err != nil || len(matches) != 1 {
		t.Fatalf("case-insensitive literal = %v, %v; want one match", matches, err)
	}
	// A regex matches the line-10..line-19 block inside the history.
	matches, searched, err := s.SearchScrollbackMode("line-1[0-9]$", true, true, 200)
	if err != nil || searched != total || len(matches) == 0 {
		t.Fatalf("regex = %v of %d, %v; want matches", matches, searched, err)
	}
	// A case-insensitive regex works too, and an invalid one is refused.
	if m, _, err := s.SearchScrollbackMode("LINE-1[0-9]$", true, false, 10); err != nil || len(m) == 0 {
		t.Fatalf("case-insensitive regex = %v, %v", m, err)
	}
	if _, _, err := s.SearchScrollbackMode("(", true, false, 10); err == nil {
		t.Fatal("an invalid regex was accepted")
	}
}

func TestSearchScrollback(t *testing.T) {
	s := startSh(t, moreLines(60))
	total := waitScrollback(t, s, func(total int) bool { return total >= 30 })
	matches, searched := s.SearchScrollback("LINE-10", 10)
	if searched != total {
		t.Fatalf("searched %d rows, want %d", searched, total)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %v, want one", matches)
	}
	lines, _ := s.Scrollback(matches[0], 1)
	if len(lines) != 1 || !strings.Contains(lines[0].Text(), "line-10") {
		t.Fatalf("the match row = %v, want line-42", lines)
	}
}

// TestSearchScrollbackIgnoresCaseOfHistoryText checks the case-insensitive literal search
// against history rows that are not lowercase.
func TestSearchScrollbackIgnoresCaseOfHistoryText(t *testing.T) {
	s := startSh(t, "i=1; while [ $i -le 60 ]; do echo Mixed-Case-$i; i=$((i+1)); done; exec sleep 5")
	waitScrollback(t, s, func(total int) bool { return total >= 30 })
	matches, _, err := s.SearchScrollbackMode("mixed-case-10", false, false, 10)
	if err != nil || len(matches) != 1 {
		t.Fatalf("case-insensitive literal = %v, %v; want one match", matches, err)
	}
	if matches, _, err := s.SearchScrollbackMode("mixed-case-10", false, true, 10); err != nil || len(matches) != 0 {
		t.Fatalf("case-sensitive literal = %v, %v; want no match", matches, err)
	}
}

// TestSessionTitleFromOSC checks the title the child sets with OSC 0/2: the last
// one wins, a reset clears it, and OSC 1 (icon name only) does not change it.
func TestSessionTitleFromOSC(t *testing.T) {
	s := startSh(t, "printf '\033]0;first title\007'; sleep 0.1; printf '\033]2;second title\007'; sleep 30")
	waitFor(t, s, "the OSC 0 title", func(Screen) bool { return s.Title() == "first title" })
	waitFor(t, s, "the OSC 2 title", func(Screen) bool { return s.Title() == "second title" })

	reset := startSh(t, "printf '\033]2;gone\007'; sleep 0.1; printf '\033]2;\007'; sleep 30")
	waitFor(t, reset, "the title reset", func(Screen) bool { return reset.Title() == "" })

	icon := startSh(t, "printf '\033]1;icon only\007'; sleep 30")
	waitFor(t, icon, "the child to print", hasText(""))
	if got := icon.Title(); got != "" {
		t.Fatalf("OSC 1 changed the title to %q", got)
	}
}

// TestCleanTitle checks the one title sanitizer: Cc/Cf stripped, trimmed and bounded
// on a rune boundary; combining marks stay.
func TestCleanTitle(t *testing.T) {
	tests := map[string]string{
		"plain":           "plain",
		"  spaced  ":      "spaced",
		"\033]0;evil\007": "]0;evil",
		"a\x00b\nc":       "abc",
		"\xff\xfeos":      "os",
		// Cf format characters (bidi overrides/isolates, zero-width markers, BOM,
		// soft hyphen) are stripped so a child cannot reorder or hide the label.
		"\u202eevil\u202c":    "evil",
		"a\u200bb\u200dc":     "abc",
		"\u2066isolate\u2069": "isolate",
		"\ufeffstart":         "start",
		"soft\u00adhyphen":    "softhyphen",
		// Combining marks are readable text, not formatting controls: keep them.
		"cafe\u0301": "cafe\u0301",
	}
	for in, want := range tests {
		if got := CleanTitle(in); got != want {
			t.Errorf("CleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", MaxTitleRunes+40)
	if got := CleanTitle(long); utf8.RuneCountInString(got) != MaxTitleRunes {
		t.Fatalf("long title length = %d, want %d", utf8.RuneCountInString(got), MaxTitleRunes)
	}
	wide := strings.Repeat("界", MaxTitleRunes+5)
	if got := CleanTitle(wide); utf8.RuneCountInString(got) != MaxTitleRunes {
		t.Fatalf("wide long title length = %d, want %d", utf8.RuneCountInString(got), MaxTitleRunes)
	}
}

// TestSessionTitleStripsFormatCharacters checks the OSC 0/2 path: bidi overrides,
// zero-width marks and other Cf format characters never reach Title()/the tab label.
func TestSessionTitleStripsFormatCharacters(t *testing.T) {
	s := startSh(t, "printf '\033]2;safe\342\200\256bidi\342\200\254\342\200\213zero\007'; sleep 30")
	t.Cleanup(func() { closeSession(t, s) })
	waitFor(t, s, "the cleaned OSC title", func(Screen) bool {
		return s.Title() == "safebidizero"
	})
	if got := s.Title(); strings.ContainsAny(got, "\u202a\u202b\u202c\u202d\u202e\u200b\u200c\u200d\u2066\u2067\u2068\u2069\ufeff") {
		t.Fatalf("Title kept a Cf format character: %q", got)
	}
}
