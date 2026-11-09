//go:build unix

package terminal

import (
	"context"
	"os"
	"path/filepath"
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

// fakePSFile is an os.FileInfo for the trusted-path resolver.
type fakePSFile struct {
	name  string
	mode  os.FileMode
	isdir bool
}

func (f fakePSFile) Name() string       { return f.name }
func (f fakePSFile) Size() int64        { return 1 }
func (f fakePSFile) Mode() os.FileMode  { return f.mode }
func (f fakePSFile) ModTime() time.Time { return time.Time{} }
func (f fakePSFile) IsDir() bool        { return f.isdir }
func (f fakePSFile) Sys() any           { return nil }

// TestResolvePSPathUsesOnlyTrustedAbsolutePaths checks the ps fallback cannot be
// redirected: it only stats the fixed absolute system candidates, takes the first
// executable regular file, and skips missing/non-executable/directory entries.
func TestResolvePSPathUsesOnlyTrustedAbsolutePaths(t *testing.T) {
	t.Parallel()
	var calls []string
	stat := func(path string) (os.FileInfo, error) {
		calls = append(calls, path)
		if path == "/usr/bin/ps" {
			return fakePSFile{name: "ps", mode: 0o555}, nil
		}
		return nil, os.ErrNotExist
	}
	if got := resolvePSPath(stat); got != "/usr/bin/ps" {
		t.Fatalf("resolved ps = %q, want /usr/bin/ps", got)
	}
	if len(calls) == 0 {
		t.Fatal("the resolver did not stat any candidate")
	}
	for _, path := range calls {
		if !filepath.IsAbs(path) {
			t.Fatalf("the resolver statted a non-absolute path %q", path)
		}
		allowed := false
		for _, trust := range trustedPSCandidates {
			if path == trust {
				allowed = true
				break
			}
		}
		if !allowed {
			t.Fatalf("the resolver statted %q outside the trusted list", path)
		}
	}
	// A non-executable file or a directory is never executed.
	for _, info := range []fakePSFile{
		{name: "ps", mode: 0o644},
		{name: "ps", mode: 0o555, isdir: true},
	} {
		if got := resolvePSPath(func(string) (os.FileInfo, error) { return info, nil }); got != "" {
			t.Fatalf("resolved %q for file %+v, want no ps", got, info)
		}
	}
	if got := resolvePSPath(func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }); got != "" {
		t.Fatalf("resolved %q with no candidates present, want none", got)
	}
}

// TestPsCommNeverRunsPsFromPATH is the regression for the PATH-injection audit:
// a directory placed first on PATH with an attacker-controlled "ps" must not be
// executed; the fallback only runs an absolute trusted system binary.
func TestPsCommNeverRunsPsFromPATH(t *testing.T) {
	// Not parallel: it changes PATH for the process.
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	malicious := "#!/bin/sh\ntouch " + marker + "\nprintf 'hacked\\n'\n"
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(malicious), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	name := psComm(os.Getpid())
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the PATH-resolved ps was executed")
	}
	if strings.Contains(name, "hacked") {
		t.Fatalf("psComm ran the untrusted ps: %q", name)
	}
}
