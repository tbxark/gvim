//go:build unix

package authority_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// fsSlot starts an in-process SSH server whose exec requests run locally through sh,
// so the real helper scripts and the real SSH quoting are under test, and returns the
// connected slot. The remote paths in these tests are local temp directories.
func fsSlot(t *testing.T) *authority.Authority {
	t.Helper()
	srv := newFakeSSH(t)
	srv.runLocally()
	return srv.connect(t, authority.SSHOptions{})
}

// writeLocal writes the local temp file a remote test operation reads.
func writeLocal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// nextFileEvent waits for a watch event matching path and kind, skipping other events
// the poll may have coalesced (create/write ordering after a burst is not asserted).
func nextFileEvent(t *testing.T, w hostos.Watch, path, kind string) hostos.WatchEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	for {
		ev, ok := w.Next(ctx)
		if !ok {
			t.Fatalf("watch ended before the %s event for %s", kind, path)
		}
		if ev.Path == path && ev.Kind == kind {
			return ev
		}
	}
}

// TestSSHFilesRead checks a remote read, the read cap and the error distinctions.
func TestSSHFilesRead(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "hello remote\\n")
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := a.Files().ReadFile(ctx, p, 1024)
	if err != nil || string(got) != "hello remote\\n" {
		t.Fatalf("ReadFile = %q, %v", got, err)
	}
	if _, err := a.Files().ReadFile(ctx, p, 4); err == nil || !strings.Contains(err.Error(), "larger than 4 bytes") {
		t.Fatalf("ReadFile over the cap = %v, want the too-large error", err)
	}
	if _, err := a.Files().ReadFile(ctx, filepath.Join(dir, "missing.txt"), 10); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file = %v, want fs.ErrNotExist", err)
	}
	if _, err := a.Files().ReadFile(ctx, dir, 10); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("ReadFile of a directory = %v, want the directory error", err)
	}
}

// TestSSHFilesWriteAtomic checks the atomic save: content and permissions, preserved
// mode, parents, an explicit mode, append, an empty file and a symlink written through.
func TestSSHFilesWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p := filepath.Join(dir, "new.txt")
	if err := a.Files().WriteFile(ctx, p, []byte("one"), hostos.WriteOptions{Atomic: true}); err != nil {
		t.Fatalf("atomic write: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "one" {
		t.Fatalf("after atomic write: %q, %v", got, err)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o644 {
		t.Fatalf("new file mode = %v, %v; want 0644", st.Mode(), err)
	}
	// An overwrite keeps the target's permission when no explicit mode is given.
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Files().WriteFile(ctx, p, []byte("two"), hostos.WriteOptions{Atomic: true}); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("overwrite mode = %v, %v; want the preserved 0600", st.Mode(), err)
	}
	// An explicit mode applies to the replacement.
	if err := a.Files().WriteFile(ctx, p, []byte("three"), hostos.WriteOptions{Atomic: true, Mode: 0o640}); err != nil {
		t.Fatalf("explicit mode write: %v", err)
	}
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o640 {
		t.Fatalf("explicit mode = %v, %v; want 0640", st.Mode(), err)
	}
	// Parents creates the missing directories.
	nested := filepath.Join(dir, "a", "b", "c.txt")
	if err := a.Files().WriteFile(ctx, nested, []byte("nested"), hostos.WriteOptions{Atomic: true, Parents: true}); err != nil {
		t.Fatalf("parents write: %v", err)
	}
	if got, err := os.ReadFile(nested); err != nil || string(got) != "nested" {
		t.Fatalf("nested file = %q, %v", got, err)
	}
	// Append adds to the existing text.
	if err := a.Files().WriteFile(ctx, p, []byte("+more"), hostos.WriteOptions{Append: true}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "three+more" {
		t.Fatalf("after append = %q", got)
	}
	// An empty atomic write creates an empty file rather than waiting for stdin.
	empty := filepath.Join(dir, "empty.txt")
	if err := a.Files().WriteFile(ctx, empty, nil, hostos.WriteOptions{Atomic: true}); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	if st, err := os.Stat(empty); err != nil || st.Size() != 0 {
		t.Fatalf("empty file = %+v, %v", st, err)
	}
	// An empty non-atomic write, like the atomic one, must not wait for stdin.
	plainEmpty := filepath.Join(dir, "plain-empty.txt")
	if err := a.Files().WriteFile(ctx, plainEmpty, nil, hostos.WriteOptions{}); err != nil {
		t.Fatalf("empty plain write: %v", err)
	}
	if st, err := os.Stat(plainEmpty); err != nil || st.Size() != 0 {
		t.Fatalf("plain empty file = %+v, %v", st, err)
	}
	// A non-atomic write through a symlink follows the link (cat semantics); the
	// atomic write resolves the link and replaces the target, leaving the link.
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	writeLocal(t, target, "old target")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := a.Files().WriteFile(ctx, link, []byte("new target"), hostos.WriteOptions{Atomic: true}); err != nil {
		t.Fatalf("atomic write through a link: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new target" {
		t.Fatalf("link target = %q, want the new content", got)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("write replaced the symlink: %v, %v", st, err)
	}

	// A non-atomic write keeps an existing file's permissions (the port ignores
	// WriteOptions.Mode on an existing file).
	plain := filepath.Join(dir, "plain.txt")
	writeLocal(t, plain, "x")
	if err := os.Chmod(plain, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.Files().WriteFile(ctx, plain, []byte("y"), hostos.WriteOptions{Mode: 0o644}); err != nil {
		t.Fatalf("plain write: %v", err)
	}
	if st, err := os.Stat(plain); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("non-atomic existing mode = %v, %v; want 0600", st.Mode(), err)
	}
}

// TestSSHFilesStat checks the stat answer kinds, size, mode and time, the follow/broken
// link behaviour and the missing-file error.
func TestSSHFilesStat(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	writeLocal(t, file, "12345")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.txt")
	if err := os.Symlink(filepath.Join(dir, "gone.txt"), broken); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st, err := a.Files().Stat(ctx, file)
	if err != nil || st.Kind != "file" || st.Size != 5 || st.MTime.IsZero() || st.Mode.Perm() != 0o644 {
		t.Fatalf("file stat = %+v, %v", st, err)
	}
	ds, err := a.Files().Stat(ctx, sub)
	if err != nil || ds.Kind != "dir" || ds.Mode&fs.ModeDir == 0 {
		t.Fatalf("dir stat = %+v, %v", ds, err)
	}
	// The port follows a link, so a live link reports its target.
	ls, err := a.Files().Stat(ctx, link)
	if err != nil || ls.Kind != "file" || ls.Size != 5 {
		t.Fatalf("link stat = %+v, %v; want the followed target", ls, err)
	}
	bs, err := a.Files().Stat(ctx, broken)
	if err != nil || bs.Kind != "symlink" {
		t.Fatalf("broken link stat = %+v, %v; want symlink", bs, err)
	}
	if _, err := a.Files().Stat(ctx, filepath.Join(dir, "missing.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing stat = %v, want fs.ErrNotExist", err)
	}
}

// TestSSHFilesReadDir checks the sorted listing, the hidden filter, entry kinds and a
// link's target kind.
func TestSSHFilesReadDir(t *testing.T) {
	dir := t.TempDir()
	writeLocal(t, filepath.Join(dir, "b.txt"), "b")
	writeLocal(t, filepath.Join(dir, "a.txt"), "a")
	writeLocal(t, filepath.Join(dir, ".hidden.txt"), "h")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "a.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entries, err := a.Files().ReadDir(ctx, dir, false)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if got, want := strings.Join(names, ","), "a.txt,b.txt,link.txt,sub"; got != want {
		t.Fatalf("names = %q, want %q", got, want)
	}
	for _, e := range entries {
		switch e.Name {
		case "sub":
			if e.Kind != "dir" {
				t.Fatalf("sub kind = %q", e.Kind)
			}
		case "link.txt":
			if e.Kind != "symlink" || e.TargetKind != "file" {
				t.Fatalf("link entry = %+v, want symlink to file", e)
			}
		case "a.txt":
			if e.Kind != "file" || e.Size != 1 || e.MTime.IsZero() {
				t.Fatalf("a.txt entry = %+v", e)
			}
		}
	}
	all, err := a.Files().ReadDir(ctx, dir, true)
	if err != nil {
		t.Fatalf("ReadDir hidden: %v", err)
	}
	foundHidden := false
	for _, e := range all {
		if e.Name == ".hidden.txt" {
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatal("hidden entry missing with hidden=true")
	}
	if _, err := a.Files().ReadDir(ctx, filepath.Join(dir, "missing"), false); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing dir = %v, want fs.ErrNotExist", err)
	}
}

// TestSSHFilesWatch checks the polled watcher reports a create, a write and a remove.
func TestSSHFilesWatch(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w, err := a.Files().Watch(ctx, dir, hostos.WatchOptions{})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Stop()
	p := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(p, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	nextFileEvent(t, w, p, "create")
	if err := os.WriteFile(p, []byte("two changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	nextFileEvent(t, w, p, "write")
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	nextFileEvent(t, w, p, "remove")
}

// TestSSHFilesErrors checks the error distinctions: a remote permission failure maps to
// fs.ErrPermission, and a failed helper never reports success.
func TestSSHFilesErrors(t *testing.T) {
	srv := newFakeSSH(t)
	srv.execFunc = func(string, io.Reader, io.Writer, io.Writer) int {
		return 1
	}
	a := srv.connect(t, authority.SSHOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.Files().ReadFile(ctx, "/x", 10); err == nil || !strings.Contains(err.Error(), "helper exited 1") {
		t.Fatalf("failed helper = %v, want the exit status error", err)
	}
	srv.execFunc = func(_ string, _ io.Reader, _ io.Writer, stderr io.Writer) int {
		_, _ = io.WriteString(stderr, "cat: /x: Permission denied\\n")
		return 1
	}
	if _, err := a.Files().ReadFile(ctx, "/x", 10); !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("permission failure = %v, want fs.ErrPermission", err)
	}
}

// TestSSHFilesQuoting checks a hostile path is data, not shell syntax: spaces, quote
// characters and a newline round-trip through the quoted helper command.
func TestSSHFilesQuoting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a b'c\"d\\ne.txt")
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	data := []byte("quoted payload\\n")
	if err := a.Files().WriteFile(ctx, p, data, hostos.WriteOptions{Atomic: true}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := a.Files().ReadFile(ctx, p, 1024)
	if err != nil || string(got) != string(data) {
		t.Fatalf("read = %q, %v", got, err)
	}
	if onDisk, err := os.ReadFile(p); err != nil || string(onDisk) != string(data) {
		t.Fatalf("on disk = %q, %v", onDisk, err)
	}
}

// TestSSHFilesDisconnected checks file I/O fails clearly when the carrier is down.
func TestSSHFilesDisconnected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "x")
	srv := newFakeSSH(t)
	srv.runLocally()
	a := srv.connect(t, authority.SSHOptions{})
	srv.dropConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := a.Files().ReadFile(ctx, p, 10); err == nil {
		t.Fatal("a remote read succeeded on a dropped carrier")
	}
}

// hugeStderrServer answers every helper with more stderr than the transport cap keeps and
// a failure exit, so the file error must say the error output was over the cap rather than
// parse a truncated message.
func hugeStderrServer(t *testing.T) *authority.Authority {
	t.Helper()
	srv := newFakeSSH(t)
	srv.execFunc = func(_ string, _ io.Reader, _ io.Writer, stderr io.Writer) int {
		chunk := make([]byte, 4096)
		for i := range chunk {
			chunk[i] = 'x'
		}
		for written := 0; written < 70_000; written += len(chunk) {
			_, _ = stderr.Write(chunk)
		}
		return 1
	}
	return srv.connect(t, authority.SSHOptions{})
}

// TestSSHFilesStderrCap checks a helper that floods stderr is rejected with an explicit
// over-cap error, never a parsed truncated message.
func TestSSHFilesStderrCap(t *testing.T) {
	a := hugeStderrServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := a.Files().ReadFile(ctx, "/x", 10)
	if err == nil || !strings.Contains(err.Error(), "error output exceeded") {
		t.Fatalf("flooded stderr = %v, want an explicit over-cap error", err)
	}
}

// listFloodServer answers a directory listing with more stdout bytes than the listing
// transport cap keeps, and a success status, so the list must be rejected as over-cap
// rather than parsed as a truncated answer.
func listFloodServer(t *testing.T) *authority.Authority {
	t.Helper()
	srv := newFakeSSH(t)
	srv.execFunc = func(_ string, _ io.Reader, stdout io.Writer, _ io.Writer) int {
		chunk := make([]byte, 64<<10)
		for i := range chunk {
			if i%3 == 0 {
				chunk[i] = 0
			} else {
				chunk[i] = 'a'
			}
		}
		for written := 0; written < 4<<20+len(chunk); written += len(chunk) {
			_, _ = stdout.Write(chunk)
		}
		return 0
	}
	return srv.connect(t, authority.SSHOptions{})
}

// TestSSHFilesListTransportCap checks a listing over the transfer cap is an explicit
// error, never a silently truncated directory.
func TestSSHFilesListTransportCap(t *testing.T) {
	a := listFloodServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := a.Files().ReadDir(ctx, "/dir", false)
	if err == nil || !strings.Contains(err.Error(), "listing exceeded") {
		t.Fatalf("flooded listing = %v, want the over-cap error", err)
	}
}

// readFloodServer answers the read helper with one byte more than the requested max and a
// success status: the read must trust the cap, not the helper's command.
func readFloodServer(t *testing.T) *authority.Authority {
	t.Helper()
	srv := newFakeSSH(t)
	srv.execFunc = func(_ string, _ io.Reader, stdout io.Writer, _ io.Writer) int {
		_, _ = stdout.Write([]byte("too long for max"))
		return 0
	}
	return srv.connect(t, authority.SSHOptions{})
}

// TestSSHFilesReadCapDetectsOverTheLimit checks a misbehaving read answer (more bytes than
// requested) is rejected as too large instead of being returned whole.
func TestSSHFilesReadCapDetectsOverTheLimit(t *testing.T) {
	a := readFloodServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := a.Files().ReadFile(ctx, "/x", 5)
	if err == nil || !strings.Contains(err.Error(), "larger than 5 bytes") {
		t.Fatalf("over-limit read = %v, want the too-large error", err)
	}
}

// TestSSHFilesWriteOverADirectoryFails: an atomic write whose target is a directory is an
// error, as the local rename is, not a temporary file moved into the directory.
func TestSSHFilesWriteOverADirectoryFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := a.Files().WriteFile(ctx, target, []byte("x"), hostos.WriteOptions{Atomic: true})
	if err == nil {
		t.Fatal("WriteFile over a directory succeeded")
	}
	entries, rerr := os.ReadDir(target)
	if rerr != nil || len(entries) != 0 {
		t.Fatalf("the directory gained entries %v (%v)", entries, rerr)
	}
}

// TestSSHWriteStreamOverADirectoryFails: a streamed atomic write whose target is a
// directory is refused at open, so nothing is renamed into the directory on commit.
func TestSSHWriteStreamOverADirectoryFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	streamer := authority.WriteStreams(a.Files())
	if streamer == nil {
		t.Fatal("the SSH backend has no streamed write")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if s, err := streamer.WriteStream(ctx, target, hostos.WriteOptions{Atomic: true}); err == nil {
		_ = s.Write(ctx, []byte("x"))
		_ = s.Commit(ctx)
		t.Fatal("WriteStream over a directory opened")
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the directory gained entries %v (%v)", entries, err)
	}
}
