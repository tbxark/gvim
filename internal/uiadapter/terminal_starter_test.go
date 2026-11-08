package uiadapter

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui"
)

// waitScreenText waits for a terminal's screen to hold text, and returns it.
func waitScreenText(t *testing.T, term tui.Terminal, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var b strings.Builder
		sc := term.Screen()
		for r := 0; r < sc.Rows; r++ {
			for c := 0; c < sc.Cols; c++ {
				ch := sc.Cell(r, c).Rune
				if ch == 0 {
					ch = ' '
				}
				b.WriteRune(ch)
			}
			b.WriteByte('\n')
		}
		if strings.Contains(b.String(), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the screen never showed %q", text)
}

func TestTermStarterStartsTheConfiguredShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the terminal is Unix only")
	}
	starter := NewTermStarter(context.Background(), TermStarterOptions{
		Shell: "/bin/sh",
		Args:  []string{"-c", "printf 'starter-%s\\n' ok; exec sleep 5"},
	})
	term, err := starter.Start(t.TempDir(), 40, 6)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = term.Close() }()
	waitScreenText(t, term, "starter-ok")
}

func TestTermStarterScrollbackLinesReachTheSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the terminal is Unix only")
	}
	script := "i=1; while [ $i -le 30 ]; do echo line-$i; i=$((i+1)); done; exec sleep 5"
	starter := NewTermStarter(context.Background(), TermStarterOptions{
		Shell: "/bin/sh", Args: []string{"-c", script}, ScrollbackLines: 0,
	})
	term, err := starter.Start(t.TempDir(), 40, 6)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = term.Close() }()
	waitScreenText(t, term, "line-30")
	if got := term.ScrollbackLen(); got != 0 {
		t.Fatalf("ScrollbackLen = %d with terminal.scrollback_lines 0, want the history disabled", got)
	}
}
