package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestRemoteGitStatusMarks checks the remote git status runs through the one-shot with the
// remote workspace cwd and the remote env delta, and parses porcelain -z into the explorer
// letters with absolute remote paths (rename/copy, conflict and untracked approximations
// included).
func TestRemoteGitStatusMarks(t *testing.T) {
	output := " M src/a.go\x00?? new.txt\x00R  moved.go\x00old.go\x00UU conflict.go\x00 D gone.go\x00!! ignored.log\x00 M dir with space/file.go\x00"
	one := &fakeOneShot{script: func(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
		if strings.Join(argv, " ") != "git status --porcelain=v1 -z" {
			return hostos.ProcResult{}, errors.New("unexpected command: " + strings.Join(argv, " "))
		}
		if o.Cwd != "/remote/w" {
			return hostos.ProcResult{}, errors.New("cwd = " + o.Cwd)
		}
		if o.Env["PATH"] != "/remote/bin" {
			return hostos.ProcResult{}, errors.New("env overlay missing PATH=/remote/bin")
		}
		return hostos.ProcResult{Stdout: []byte(output)}, nil
	}}
	g := newRemoteGitStatus(one, "/remote/w", func() []string { return []string{"PATH=/remote/bin"} })
	// A caller passing a local-looking dir must not redirect the read to the local host.
	marks, err := g.Marks("/local/decoy")
	if err != nil {
		t.Fatalf("Marks: %v", err)
	}
	want := map[string]rune{
		"/remote/w/src/a.go":               'M',
		"/remote/w/new.txt":                'U',
		"/remote/w/moved.go":               'R',
		"/remote/w/conflict.go":            'C',
		"/remote/w/gone.go":                'D',
		"/remote/w/dir with space/file.go": 'M',
	}
	if len(marks) != len(want) {
		t.Fatalf("marks = %v, want %v", marks, want)
	}
	for path, letter := range want {
		if marks[path] != letter {
			t.Fatalf("marks[%q] = %q, want %q (all %v)", path, marks[path], letter, marks)
		}
	}
	if _, ok := marks["/remote/w/old.go"]; ok {
		t.Fatal("the rename original path was marked")
	}
	if _, ok := marks["/remote/w/ignored.log"]; ok {
		t.Fatal("an ignored path was marked")
	}
	calls := one.recorded()
	if len(calls) != 1 || calls[0].cwd != "/remote/w" {
		t.Fatalf("calls = %+v", calls)
	}
}

// TestRemoteGitStatusCacheAndInvalidate checks a burst of reads is coalesced, the cache
// expires, and Invalidate forces the next read (the reconnect path).
func TestRemoteGitStatusCacheAndInvalidate(t *testing.T) {
	now := time.Unix(1000, 0)
	read := 0
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		read++
		return hostos.ProcResult{Stdout: []byte(" M a.go\x00")}, nil
	}}
	g := newRemoteGitStatus(one, "/remote/w", nil)
	g.now = func() time.Time { return now }
	g.ttl = time.Second
	for i := 0; i < 3; i++ {
		if _, err := g.Marks(""); err != nil {
			t.Fatal(err)
		}
	}
	if read != 1 {
		t.Fatalf("reads = %d, want one cached read", read)
	}
	now = now.Add(2 * time.Second)
	if _, err := g.Marks(""); err != nil {
		t.Fatal(err)
	}
	if read != 2 {
		t.Fatalf("reads after TTL = %d, want 2", read)
	}
	g.Invalidate()
	if _, err := g.Marks(""); err != nil {
		t.Fatal(err)
	}
	if read != 3 {
		t.Fatalf("reads after Invalidate = %d, want 3", read)
	}
}

// TestRemoteGitStatusErrors checks a failing git status is an error (never a silent empty
// status) and a timeout is reported within the bound.
func TestRemoteGitStatusErrors(t *testing.T) {
	one := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{Code: 128, Stderr: []byte("fatal: not a git repository")}, nil
	}}
	g := newRemoteGitStatus(one, "/remote/w", nil)
	if _, err := g.Marks(""); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("error = %v", err)
	}
	// A timeout is bounded and becomes the notice error.
	slow := &fakeOneShot{script: func(ctx context.Context, _ []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		<-ctx.Done()
		return hostos.ProcResult{}, ctx.Err()
	}}
	g = newRemoteGitStatus(slow, "/remote/w", nil)
	g.timeout = 30 * time.Millisecond
	start := time.Now()
	if _, err := g.Marks(""); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("the timeout took %s", time.Since(start))
	}
}

// TestInstallRemoteDataPorts checks the remote wiring installs the authority-backed git
// status and preview ports (the reconnect hook gets the git port back for invalidation).
func TestInstallRemoteDataPorts(t *testing.T) {
	fs := &previewFS{data: []byte("x")}
	slot := previewSlot(fs, true)
	ports := tui.RootPorts{}
	git := installRemoteDataPorts(&ports, slot, "/remote/w", func() []string { return []string{"PATH=/remote/bin"} })
	if git == nil || ports.Git != git {
		t.Fatalf("git port = %v, returned %v", ports.Git, git)
	}
	if _, ok := ports.Preview.(*remotePreviewer); !ok {
		t.Fatalf("preview port = %T, want the remote previewer", ports.Preview)
	}
	if git.root != "/remote/w" {
		t.Fatalf("git root = %q", git.root)
	}
}
