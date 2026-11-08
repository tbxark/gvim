//go:build unix

package authority_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// streamDirs lists the temporary files a streamed write leaves in dir.
func streamTemps(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".gvim-") {
			out = append(out, entry.Name())
		}
	}
	return out
}

// TestSSHFilesWriteStreamCommits checks the remote streamed atomic write: the target is
// untouched while chunks are written, the commit replaces it with the concatenated chunks,
// the existing mode is preserved and no temporary file remains.
func TestSSHFilesWriteStreamCommits(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "old\n")
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	streamer := authority.WriteStreams(a.Files())
	if streamer == nil {
		t.Fatal("the SSH file backend does not implement WriteStreamer")
	}
	s, err := streamer.WriteStream(ctx, p, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	if err := s.Write(ctx, []byte("new ")); err != nil {
		s.Abort(ctx)
		t.Fatalf("chunk 1: %v", err)
	}
	// The original file is still the old content until the commit.
	if got, _ := os.ReadFile(p); string(got) != "old\n" {
		t.Fatalf("target = %q before commit, want the original", got)
	}
	if err := s.Write(ctx, []byte("content\n")); err != nil {
		s.Abort(ctx)
		t.Fatalf("chunk 2: %v", err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new content\n" {
		t.Fatalf("target = %q after commit", got)
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode after streamed commit = %v, %v; want 0600 preserved", info.Mode().Perm(), err)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after commit: %v", temps)
	}
}

// TestSSHFilesWriteStreamAbort checks an abort removes the temporary file and leaves the
// original untouched.
func TestSSHFilesWriteStreamAbort(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "old\n")
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := authority.WriteStreams(a.Files()).WriteStream(ctx, p, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	if err := s.Write(ctx, []byte("partial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	s.Abort(ctx)
	if got, _ := os.ReadFile(p); string(got) != "old\n" {
		t.Fatalf("target changed after abort: %q", got)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after abort: %v", temps)
	}
}

// TestSSHFilesWriteStreamCancellation checks a cancelled write is an error and a cleanup
// with a fresh context removes the temporary file.
func TestSSHFilesWriteStreamCancellation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "old\n")
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := authority.WriteStreams(a.Files()).WriteStream(ctx, p, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	cancelled, ccancel := context.WithCancel(context.Background())
	ccancel()
	if err := s.Write(cancelled, []byte("x")); err == nil {
		t.Fatal("a cancelled chunk was accepted")
	}
	s.Abort(context.Background())
	if got, _ := os.ReadFile(p); string(got) != "old\n" {
		t.Fatalf("target changed after a cancelled stream: %q", got)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("temporary files left after cancellation: %v", temps)
	}
}

// TestSSHFilesWriteStreamSymlink checks a streamed write through a symbolic link keeps the
// link and replaces its target, like the atomic whole-file write.
func TestSSHFilesWriteStreamSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	writeLocal(t, target, "old\n")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := authority.WriteStreams(a.Files()).WriteStream(ctx, link, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("WriteStream: %v", err)
	}
	if err := s.Write(ctx, []byte("new\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symbolic link was replaced: mode %v, %v", fi.Mode(), err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new\n" {
		t.Fatalf("target after streamed symlink write = %q", got)
	}
}

// TestSSHFilesWriteStreamRefusesOptions checks the stream refuses the write options it does
// not implement instead of silently differing from WriteFile.
func TestSSHFilesWriteStreamRefusesOptions(t *testing.T) {
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, o := range []hostos.WriteOptions{
		{Atomic: false},
		{Atomic: true, Append: true},
		{Atomic: true, Parents: true},
	} {
		if _, err := authority.WriteStreams(a.Files()).WriteStream(ctx, filepath.Join(t.TempDir(), "x.txt"), o); !errors.Is(err, authority.ErrUnsupportedFileIO) {
			t.Fatalf("options %+v = %v, want ErrUnsupportedFileIO", o, err)
		}
	}
}

// TestLocalWriteStream checks the local implementation: chunked temp file, commit rename
// and abort cleanup.
func TestLocalWriteStream(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	slot := authority.Local(authority.LocalOptions{})
	defer func() { _ = slot.Close() }()
	ctx := context.Background()
	s, err := authority.WriteStreams(slot.Files()).WriteStream(ctx, p, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("local WriteStream: %v", err)
	}
	if err := s.Write(ctx, []byte("new")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Fatalf("target before commit = %q", got)
	}
	if err := s.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("target after commit = %q", got)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("local temporary files left: %v", temps)
	}
	// Abort path.
	s, err = authority.WriteStreams(slot.Files()).WriteStream(ctx, p, hostos.WriteOptions{Atomic: true})
	if err != nil {
		t.Fatalf("local WriteStream 2: %v", err)
	}
	if err := s.Write(ctx, []byte("discarded")); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	s.Abort(ctx)
	if got, _ := os.ReadFile(p); string(got) != "new" {
		t.Fatalf("target after abort = %q", got)
	}
	if temps := streamTemps(t, dir); len(temps) != 0 {
		t.Fatalf("local temporary files left after abort: %v", temps)
	}
}
