package uiadapter

import (
	"context"
	"errors"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// dirFS is a FileSystem whose Stat is scripted and whose other operations refuse.
type dirFS struct {
	stats map[string]hostos.FileStat
	err   error
	calls int
}

func (f *dirFS) ReadFile(context.Context, string, int64) ([]byte, error) {
	return nil, errors.New("not scripted")
}

func (f *dirFS) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not scripted")
}

func (f *dirFS) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return errors.New("not scripted")
}

func (f *dirFS) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.calls++
	if f.err != nil {
		return hostos.FileStat{}, f.err
	}
	st, ok := f.stats[path]
	if !ok {
		return hostos.FileStat{}, errors.New("no such directory")
	}
	return st, nil
}

func (f *dirFS) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, errors.New("not scripted")
}

func (f *dirFS) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not scripted")
}

// dirTerminal records the directory a launch was asked to use and refuses to start.
type dirTerminal struct {
	dirs []string
}

func (d *dirTerminal) Launch(_ context.Context, o authority.TerminalOptions) (authority.TerminalLaunch, error) {
	d.dirs = append(d.dirs, o.Dir)
	return authority.TerminalLaunch{}, errors.New("launch stopped by the test")
}

// dirSlot is an authority slot whose FileSystem/remote state are scripted.
type dirSlot struct {
	fs        authority.FileSystem
	remote    *authority.RemoteSpec
	connected bool
	term      authority.Terminal
}

func (s *dirSlot) Files() authority.FileSystem        { return s.fs }
func (s *dirSlot) OneShot() authority.OneShot         { return authority.UnsupportedProcs{} }
func (s *dirSlot) LongRunning() authority.LongRunning { return authority.UnsupportedProcs{} }
func (s *dirSlot) Terminal() authority.Terminal       { return s.term }
func (s *dirSlot) Label() string                      { return "remote" }
func (s *dirSlot) Connected() bool                    { return s.connected }
func (s *dirSlot) Reconnect(context.Context) error    { return nil }
func (s *dirSlot) Close() error                       { return nil }
func (s *dirSlot) State() authority.State {
	return authority.State{Kind: "ssh", Remote: s.remote, Connected: s.connected}
}

func dirStarter(slot authority.Slot, notices *[]string) TermStarter {
	*notices = nil
	return NewTermStarter(context.Background(), TermStarterOptions{
		Authority: slot,
		Notice:    func(text string) { *notices = append(*notices, text) },
	})
}

// TestCheckedRemoteDir checks the pre-launch directory validation: a remote saved
// cwd is used when the authority confirms a directory, and otherwise reported and
// replaced by "" (the authority's default) without touching the local file system;
// local slots are never validated.
func TestCheckedRemoteDir(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	remote := &authority.RemoteSpec{Host: "example.test"}
	validFS := &dirFS{stats: map[string]hostos.FileStat{"/work": {Kind: "dir"}}}
	missingFS := &dirFS{}
	fileFS := &dirFS{stats: map[string]hostos.FileStat{"/work": {Kind: "file"}}}

	var notices []string
	slot := &dirSlot{fs: validFS, remote: remote, connected: true}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, "/work"); got != "/work" {
		t.Fatalf("valid remote dir = %q", got)
	}
	if len(notices) != 0 {
		t.Fatalf("valid remote dir notices = %q", notices)
	}

	slot = &dirSlot{fs: missingFS, remote: remote, connected: true}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, "/gone"); got != "" {
		t.Fatalf("missing remote dir = %q, want the authority default", got)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Remote terminal directory") || !strings.Contains(notices[0], "/gone") {
		t.Fatalf("missing remote dir notices = %q", notices)
	}

	slot = &dirSlot{fs: fileFS, remote: remote, connected: true}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, "/work"); got != "" || len(notices) != 1 {
		t.Fatalf("file remote dir = %q notices %q", got, notices)
	}

	slot = &dirSlot{fs: validFS, remote: remote, connected: false}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, "/work"); got != "" || len(notices) != 1 {
		t.Fatalf("disconnected remote dir = %q notices %q", got, notices)
	}
	if validFS.calls != 1 {
		t.Fatalf("disconnected authority stat calls = %d, want no stat", validFS.calls)
	}

	// A Windows-flavoured saved path must never be used as a remote POSIX start
	// cwd, and it is refused before any authority round trip.
	windowsFS := &dirFS{stats: map[string]hostos.FileStat{`C:work`: {Kind: "dir"}}}
	slot = &dirSlot{fs: windowsFS, remote: remote, connected: true}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, `C:work`); got != "" {
		t.Fatalf("Windows remote dir = %q, want the remote default", got)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "not a POSIX path") {
		t.Fatalf("Windows remote dir notices = %q", notices)
	}
	if windowsFS.calls != 0 {
		t.Fatalf("Windows remote dir stat calls = %d, want none", windowsFS.calls)
	}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, `\servershare`); got != "" || len(notices) != 1 {
		t.Fatalf("UNC remote dir = %q notices %q", got, notices)
	}

	// Local slots are untouched: no stat, no notice, the directory passes through.
	localFS := &dirFS{}
	slot = &dirSlot{fs: localFS, remote: nil, connected: true}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, "/local\x1bdir"); got != "/local\x1bdir" {
		t.Fatalf("local dir = %q", got)
	}
	if localFS.calls != 0 || len(notices) != 0 {
		t.Fatalf("local dir stats %d notices %q", localFS.calls, notices)
	}
	if got := dirStarter(slot, &notices).checkedRemoteDir(ctx, ""); got != "" || len(notices) != 0 {
		t.Fatalf("empty dir = %q notices %q", got, notices)
	}
}

// TestRemoteStartFallsBackToTheDefaultDirectory checks the start path itself: an
// unavailable remote cwd reaches the authority launch as "" (default) with a
// notice, a valid one is kept, and a local-start path is unchanged.
func TestRemoteStartFallsBackToTheDefaultDirectory(t *testing.T) {
	t.Parallel()
	remote := &authority.RemoteSpec{Host: "example.test"}
	term := &dirTerminal{}
	var notices []string
	starter := dirStarter(&dirSlot{
		fs:     &dirFS{stats: map[string]hostos.FileStat{"/work": {Kind: "dir"}}},
		remote: remote, connected: true, term: term,
	}, &notices)
	if _, err := starter.Start("/gone", 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 1 || term.dirs[0] != "" {
		t.Fatalf("launch dirs = %v, want the authority default", term.dirs)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Remote terminal directory") {
		t.Fatalf("notices = %q", notices)
	}
	if _, err := starter.Start("/work", 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 2 || term.dirs[1] != "/work" {
		t.Fatalf("launch dirs = %v, want the valid remote directory", term.dirs)
	}
	if _, err := starter.Start(`C:\work`, 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 3 || term.dirs[2] != "" {
		t.Fatalf("launch dirs = %v, want no remote launch of a Windows path", term.dirs)
	}

	// A local (non-remote) authority keeps the local directory behavior.
	localTerm := &dirTerminal{}
	local := dirStarter(&dirSlot{fs: &dirFS{}, connected: true, term: localTerm}, &notices)
	if _, err := local.Start("/local/dir", 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(localTerm.dirs) != 1 || localTerm.dirs[0] != "/local/dir" {
		t.Fatalf("local launch dirs = %v, want the local path unchanged", localTerm.dirs)
	}
}

// TestDirAbsoluteForPlatform checks the two path conventions explicitly, so the
// Windows behaviour is covered even though this test runs on a Unix host.
func TestDirAbsoluteForPlatform(t *testing.T) {
	t.Parallel()
	tests := []struct {
		dir, goos string
		want      bool
	}{
		{"/tmp/x", "linux", true},
		{"/tmp/x", "darwin", true},
		{"/tmp/x", "windows", false},
		{`C:\Users\me`, "windows", true},
		{`C:/Users/me`, "windows", true},
		{`\\server\share`, "windows", true},
		{`C:\Users\me`, "linux", false},
		{`C:/Users/me`, "darwin", false},
		{`\\server\share`, "linux", false},
		{"C:dir", "windows", false}, // drive-relative is not absolute
		{"C:", "windows", false},
		{"dir", "windows", false},
		{"dir", "linux", false},
	}
	for _, tt := range tests {
		if got := dirAbsoluteForPlatform(tt.dir, tt.goos); got != tt.want {
			t.Errorf("dirAbsoluteForPlatform(%q, %q) = %v, want %v", tt.dir, tt.goos, got, tt.want)
		}
	}
}

// TestLocalStartRejectsCrossPlatformDir checks the local launch path keeps its
// platform's semantics: on a Unix test host a saved Windows cwd is reported and
// replaced by the default local directory before any PTY launch, while a POSIX
// cwd passes through untouched.
func TestLocalStartRejectsCrossPlatformDir(t *testing.T) {
	t.Parallel()
	if goruntime.GOOS == "windows" {
		t.Skip("the local flavour check runs against a Unix host in this test")
	}
	term := &dirTerminal{}
	var notices []string
	starter := dirStarter(&dirSlot{fs: &dirFS{}, connected: true, term: term}, &notices)
	if _, err := starter.Start(`C:\Users\me`, 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 1 || term.dirs[0] != "" {
		t.Fatalf("Windows local launch dirs = %v, want the default directory", term.dirs)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "not an absolute path on this platform") {
		t.Fatalf("Windows local notices = %q", notices)
	}
	if _, err := starter.Start(`\\server\share`, 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 2 || term.dirs[1] != "" {
		t.Fatalf("UNC local launch dirs = %v, want the default directory", term.dirs)
	}
	if _, err := starter.Start("/tmp/work", 40, 6); err == nil {
		t.Fatal("Start did not return the scripted launch error")
	}
	if len(term.dirs) != 3 || term.dirs[2] != "/tmp/work" {
		t.Fatalf("POSIX local launch dirs = %v, want the POSIX path", term.dirs)
	}
}
