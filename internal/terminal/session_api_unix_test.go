//go:build unix

package terminal

import (
	"context"
	"strings"
	"testing"
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
