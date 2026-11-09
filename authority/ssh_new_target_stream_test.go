//go:build unix

package authority_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

func newTargetStream(t *testing.T, a *authority.Authority) authority.NewTargetWriteStreamer {
	t.Helper()
	streamer := authority.NewTargetWriteStreams(a.Files())
	if streamer == nil {
		t.Fatal("the SSH file backend has no NewTargetWriteStreamer")
	}
	return streamer
}

func waitCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 20*time.Second)
}

// TestSSHNewTargetStreamCommits checks the happy path: the destination does not exist
// while chunks are streamed, the hardlink commit publishes one complete inode with the
// requested mode, and only its temporary file is removed.
func TestSSHNewTargetStreamCommits(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	streamer := newTargetStream(t, a)
	s, err := streamer.WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true, Mode: 0o640})
	if err != nil {
		t.Fatalf("WriteStreamNewTarget: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target exists before commit: %v", err)
	}
	if err := s.Write(ctx, []byte("hello ")); err != nil {
		s.Abort(ctx)
		t.Fatalf("chunk 1: %v", err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("target exists during stream: %v", err)
	}
	if err := s.Write(ctx, []byte("new target")); err != nil {
		s.Abort(ctx)
		t.Fatalf("chunk 2: %v", err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "hello new target" {
		t.Fatalf("target = %q", got)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("target mode = %v, %v", info.Mode().Perm(), err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
}

// TestSSHNewTargetStreamExistingEntriesConflict: every already-existing target kind
// (file, directory, symlink, dangling symlink) is EEXIST and is left exactly as it was.
func TestSSHNewTargetStreamExistingEntriesConflict(t *testing.T) {
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	streamer := newTargetStream(t, a)
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(regular, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	dirlink := filepath.Join(dir, "dirlink")
	if err := os.Symlink(sub, dirlink); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{regular, link, dangling, sub, dirlink} {
		s, err := streamer.WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true})
		if err != nil {
			t.Fatalf("%s: open: %v", target, err)
		}
		if err := s.Write(ctx, []byte("replacement")); err != nil {
			t.Fatalf("%s: write: %v", target, err)
		}
		if err := s.Commit(ctx); err == nil || !errors.Is(err, fs.ErrExist) {
			t.Fatalf("%s: commit = %v, want fs.ErrExist", target, err)
		}
	}
	if got, _ := os.ReadFile(regular); string(got) != "keep" {
		t.Fatalf("regular target changed: %q", got)
	}
	if got, _ := os.ReadFile(real); string(got) != "real" {
		t.Fatalf("symlink target changed: %q", got)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("existing symlink changed: %v, %v", fi.Mode(), err)
	}
	if fi, err := os.Lstat(dangling); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling symlink changed: %v, %v", fi.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("dangling target was created: %v", err)
	}
	if fi, err := os.Stat(sub); err != nil || !fi.IsDir() {
		t.Fatalf("directory target changed: %v, %v", fi, err)
	}
	if fi, err := os.Lstat(dirlink); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("directory symlink changed: %v, %v", fi.Mode(), err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
}

// TestSSHNewTargetStreamConcurrentCreator: a writer that creates the destination after
// the stream opened wins; link commit reports conflict and the winner is untouched.
func TestSSHNewTargetStreamConcurrentCreator(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	s, err := newTargetStream(t, a).WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Write(ctx, []byte("copy data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(target, []byte("winner"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx); err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("commit after concurrent create = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "winner" {
		t.Fatalf("concurrent winner changed: %q", got)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
}

// TestSSHNewTargetStreamAbortAndCancel: abort/cancel remove only the temp; no target is
// created and an existing target is untouched.
func TestSSHNewTargetStreamAbortAndCancel(t *testing.T) {
	a := fsSlot(t)
	streamer := newTargetStream(t, a)
	dir := t.TempDir()
	ctx, cancel := waitCtx(t)
	defer cancel()

	aborted := filepath.Join(dir, "aborted.txt")
	s, err := streamer.WriteStreamNewTarget(ctx, aborted, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Write(ctx, []byte("discard")); err != nil {
		t.Fatal(err)
	}
	s.Abort(ctx)
	if _, err := os.Lstat(aborted); !os.IsNotExist(err) {
		t.Fatalf("aborted target exists: %v", err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after abort: %v", temps)
	}

	cancelled := filepath.Join(dir, "cancelled.txt")
	s, err = streamer.WriteStreamNewTarget(ctx, cancelled, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatal(err)
	}
	bad, badCancel := context.WithCancel(context.Background())
	badCancel()
	if err := s.Write(bad, []byte("discard")); err == nil {
		t.Fatal("cancelled chunk was accepted")
	}
	s.Abort(context.Background())
	if _, err := os.Lstat(cancelled); !os.IsNotExist(err) {
		t.Fatalf("cancelled target exists: %v", err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after cancel: %v", temps)
	}
}

// TestSSHNewTargetStreamMultiChunk: many chunks still commit one complete target.
func TestSSHNewTargetStreamMultiChunk(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "large.txt")
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	s, err := newTargetStream(t, a).WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for i := 0; i < 8; i++ {
		chunk := strings.Repeat(string(rune('a'+i)), 4096)
		want += chunk
		if err := s.Write(ctx, []byte(chunk)); err != nil {
			t.Fatalf("chunk %d: %v", i, err)
		}
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != want {
		t.Fatalf("large target differs (%d bytes)", len(got))
	}
}

// TestSSHNewTargetStreamConcurrentDirectory: a directory created at the destination after
// open is EEXIST and is left in place; link must not create a name inside it.
func TestSSHNewTargetStreamConcurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	s, err := newTargetStream(t, a).WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Write(ctx, []byte("copy data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.Commit(ctx); err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("commit over a concurrent directory = %v, want fs.ErrExist", err)
	}
	if fi, err := os.Stat(target); err != nil || !fi.IsDir() {
		t.Fatalf("concurrent directory changed: %v, %v", fi, err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left: %v", temps)
	}
}

// TestSSHNewTargetStreamCommitCancellation: a cancelled commit aborts only its temp and
// never creates the target.
func TestSSHNewTargetStreamCommitCancellation(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cancelled-commit.txt")
	a := fsSlot(t)
	ctx, cancel := waitCtx(t)
	defer cancel()
	s, err := newTargetStream(t, a).WriteStreamNewTarget(ctx, target, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Write(ctx, []byte("data")); err != nil {
		t.Fatalf("write: %v", err)
	}
	cancelled, ccancel := context.WithCancel(context.Background())
	ccancel()
	if err := s.Commit(cancelled); err == nil {
		t.Fatal("cancelled commit succeeded")
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("cancelled commit created a target: %v", err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after cancelled commit: %v", temps)
	}
}
