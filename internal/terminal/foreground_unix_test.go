//go:build unix

package terminal

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestForegroundProcessNameOfALocalShell checks the polling primitive Fresh uses
// for tab titles: tcgetpgrp + /proc (Linux) or a bounded ps (other Unix) names the
// foreground process after the shell execs into it. This test runs on the host's
// real PTY and is skipped nowhere in the Unix build: if ps is missing the read
// returns false and the dock falls back to the recorded title.
func TestForegroundProcessNameOfALocalShell(t *testing.T) {
	s := startSh(t, "printf 'fg-ready\n'; exec sleep 30")
	t.Cleanup(func() { closeSession(t, s) })
	waitFor(t, s, "the foreground marker", hasText("fg-ready"))
	if !s.ForegroundProcessSupported() {
		t.Fatal("a local Unix PTY reported no foreground-process support")
	}
	deadline := time.Now().Add(3 * time.Second)
	var name string
	for time.Now().Before(deadline) {
		if got, ok := s.ForegroundProcessName(); ok && strings.Contains(strings.ToLower(got), "sleep") {
			name = got
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if name == "" {
		t.Fatalf("ForegroundProcessName never reported the sleep child (supported %v)", s.ForegroundProcessSupported())
	}
	if strings.ContainsAny(name, "\x00\n\r") || strings.TrimSpace(name) != name {
		t.Fatalf("foreground name is not cleaned: %q", name)
	}
}

// TestForegroundProcessNameUnsupportedTransport checks a remote/backend session
// degrades safely: the capability is false and no name is fabricated, so the dock
// keeps the recorded launch/OSC title and never polls.
func TestForegroundProcessNameUnsupportedTransport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newFakeBackend()
	s, err := StartWithBackend(ctx, Options{Cols: 80, Rows: 24}, b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeSession(t, s) })
	if s.ForegroundProcessSupported() {
		t.Fatal("a backend transport claimed foreground-process support")
	}
	if name, ok := s.ForegroundProcessName(); ok || name != "" {
		t.Fatalf("ForegroundProcessName on a backend = %q, %v", name, ok)
	}
}

// TestForegroundProcessNameAfterClose checks a closed session never reads (the fd
// may already be reused) and reports no capability or name.
func TestForegroundProcessNameAfterClose(t *testing.T) {
	s := startSh(t, "exec sleep 30")
	closeSession(t, s)
	if s.ForegroundProcessSupported() {
		t.Fatal("a closed session reported foreground-process support")
	}
	if name, ok := s.ForegroundProcessName(); ok || name != "" {
		t.Fatalf("ForegroundProcessName after Close = %q, %v", name, ok)
	}
}

// TestSanitizeForegroundProcessName checks the one sanitizer an OS process name
// goes through before it can reach a tab label: controls and invalid UTF-8 are
// dropped, outer space trimmed, and the name bounded to 64 runes.
func TestSanitizeForegroundProcessName(t *testing.T) {
	tests := map[string]string{
		"python3":         "python3",
		"  bash  ":        "bash",
		"evil\x01\x1b]0;": "evil]0;",
		"line\nbreak":     "linebreak",
		"\xff\xfeos":      "os",
	}
	for in, want := range tests {
		if got := sanitizeForegroundProcessName(in); got != want {
			t.Errorf("sanitizeForegroundProcessName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 100)
	if got := sanitizeForegroundProcessName(long); len([]rune(got)) != 64 {
		t.Fatalf("long name length = %d, want 64", len([]rune(got)))
	}
}
