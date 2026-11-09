//go:build unix

package authority_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// createDirs lists the streamed-write temporary files a create helper could leave in dir.
func createTemps(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, entry := range entries {
		if len(entry.Name()) > 0 && entry.Name()[0] == '.' && (contains(entry.Name(), ".gvim-") || contains(entry.Name(), "create")) {
			out = append(out, entry.Name())
		}
	}
	return out
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSSHFilesCreateNew checks the exclusive create over the real helper scripts: a new
// empty file (mode honored), an existing file/dir/symlink refused with fs.ErrExist and
// never changed, and a missing parent reported as an error.
func TestSSHFilesCreateNew(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creator := authority.Creators(a.Files())
	if creator == nil {
		t.Fatal("the SSH file backend does not implement FileCreator")
	}
	p := filepath.Join(dir, "new.txt")
	if err := creator.CreateNew(ctx, p, hostos.WriteOptions{}); err != nil {
		t.Fatalf("CreateNew: %v", err)
	}
	if info, err := os.Stat(p); err != nil || info.Size() != 0 {
		t.Fatalf("created file = %v, %v; want an empty file", info.Size(), err)
	}
	// The second create refuses and must not change the existing entry.
	if err := os.WriteFile(p, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNew(ctx, p, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over an existing file = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "precious" {
		t.Fatalf("existing file changed to %q", got)
	}
	// A dangling symlink is an existing entry: not followed, not replaced, target missing.
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNew(ctx, dangling, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over a dangling symlink = %v, want fs.ErrExist", err)
	}
	if fi, err := os.Lstat(dangling); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling symlink changed: %v, %v", fi.Mode(), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatalf("the dangling target was created: %v", err)
	}
	// An existing directory is an existing entry too.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNew(ctx, sub, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over a directory = %v, want fs.ErrExist", err)
	}
	// A missing parent directory fails (a create-new does not create ancestors).
	if err := creator.CreateNew(ctx, filepath.Join(dir, "no", "such", "a.txt"), hostos.WriteOptions{}); err == nil {
		t.Fatal("create below a missing parent succeeded")
	}
	// An explicit mode is applied.
	modePath := filepath.Join(dir, "mode.txt")
	if err := creator.CreateNew(ctx, modePath, hostos.WriteOptions{Mode: 0o600}); err != nil {
		t.Fatalf("CreateNew mode: %v", err)
	}
	if info, err := os.Stat(modePath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("created mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	if temps := createTemps(t, dir); len(temps) != 0 {
		t.Fatalf("create left temporary files: %v", temps)
	}
}

// TestSSHFilesCreateNewConcurrent checks concurrent creators: exactly one wins, the rest
// report fs.ErrExist and the target stays the empty file the winner created.
func TestSSHFilesCreateNewConcurrent(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	creator := authority.Creators(a.Files())
	p := filepath.Join(dir, "race.txt")
	const n = 16
	var wg sync.WaitGroup
	results := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- creator.CreateNew(ctx, p, hostos.WriteOptions{})
		}()
	}
	wg.Wait()
	close(results)
	wins, exists := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, fs.ErrExist):
			exists++
		default:
			t.Fatalf("unexpected concurrent create error: %v", err)
		}
	}
	if wins != 1 || exists != n-1 {
		t.Fatalf("wins = %d, exists = %d; want 1 and %d", wins, exists, n-1)
	}
	if got, err := os.ReadFile(p); err != nil || len(got) != 0 {
		t.Fatalf("raced target = %q, %v; want the empty created file", got, err)
	}
}

// TestSSHFilesCreateNewHelperFailureKeepsExisting checks a failed helper never deletes an
// entry that already existed (the chmod-failure path leaves the path alone too).
func TestSSHFilesCreateNewHelperFailureKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "precious")
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := authority.Creators(a.Files()).CreateNew(ctx, p, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over an existing file = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "precious" {
		t.Fatalf("the existing file was deleted or changed: %q", got)
	}
}

// TestLocalCreateNew checks the local backend's O_EXCL create, refusal and concurrency.
func TestLocalCreateNew(t *testing.T) {
	dir := t.TempDir()
	slot := authority.Local(authority.LocalOptions{})
	defer func() { _ = slot.Close() }()
	ctx := context.Background()
	creator := authority.Creators(slot.Files())
	if creator == nil {
		t.Fatal("the local file backend does not implement FileCreator")
	}
	p := filepath.Join(dir, "a.txt")
	if err := creator.CreateNew(ctx, p, hostos.WriteOptions{}); err != nil {
		t.Fatalf("local CreateNew: %v", err)
	}
	if err := os.WriteFile(p, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNew(ctx, p, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("local create over existing = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(p); string(got) != "precious" {
		t.Fatalf("local existing file changed: %q", got)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNew(ctx, dangling, hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("local create over dangling symlink = %v, want fs.ErrExist", err)
	}
}

// TestUnsupportedFilesHasNoCreator checks a backend without the exclusive-create surface is
// distinguishable (nil), which is what makes the explorer refuse instead of racing.
func TestUnsupportedFilesHasNoCreator(t *testing.T) {
	if creator := authority.Creators(authority.UnsupportedFiles{}); creator != nil {
		t.Fatal("UnsupportedFiles unexpectedly implements FileCreator")
	}
}

// TestSSHFilesCreateNewChmodFailureKeepsFile checks the helper failure path after the
// exclusive create: a failing chmod must report an error but must NEVER delete the path,
// because between the create and the chmod another process could have replaced it. The
// test shadows chmod with a failing one in the shell's PATH.
func TestSSHFilesCreateNewChmodFailureKeepsFile(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "chmod"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p := filepath.Join(dir, "owned.txt")
	err := authority.Creators(a.Files()).CreateNew(ctx, p, hostos.WriteOptions{Mode: 0o600})
	if err == nil {
		t.Skip("the failing chmod was not exercised (PATH shadowing did not apply)")
	}
	// The file this helper created stays: no helper failure deletes a path another
	// process could own by then.
	fi, statErr := os.Stat(p)
	if statErr != nil {
		t.Fatalf("the helper deleted the created file on a chmod failure: %v", statErr)
	}
	if fi.Size() != 0 {
		t.Fatalf("the created file has %d bytes, want empty", fi.Size())
	}
}

// TestSSHFilesCreateNewWithContent checks the single-op exclusive content creator over the
// real helper shell: exact bytes (including NUL/newlines), mode, existing-entry refusal,
// dangling-symlink refusal and concurrent creators where exactly one wins.
func TestSSHFilesCreateNewWithContent(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	creator := authority.ContentCreators(a.Files())
	if creator == nil {
		t.Fatal("the SSH file backend does not implement FileContentCreator")
	}
	p := filepath.Join(dir, "config.toml")
	data := []byte("assembler = \"gas\"\n# nul: \x00\n")
	if err := creator.CreateNewWithContent(ctx, p, data, hostos.WriteOptions{Mode: 0o600}); err != nil {
		t.Fatalf("CreateNewWithContent: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != string(data) {
		t.Fatalf("content = %q, %v; want %q", got, err, data)
	}
	if info, err := os.Stat(p); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 0600", info, err)
	}
	if err := creator.CreateNewWithContent(ctx, p, []byte("theirs"), hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over existing = %v, want fs.ErrExist", err)
	}
	if got, _ := os.ReadFile(p); string(got) != string(data) {
		t.Fatalf("refused create changed the file: %q", got)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Fatal(err)
	}
	if err := creator.CreateNewWithContent(ctx, dangling, []byte("x"), hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over dangling symlink = %v, want fs.ErrExist", err)
	}
	if fi, err := os.Lstat(dangling); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling symlink changed: %v, %v", fi, err)
	}
	// Concurrent creators: exactly one claim wins and its content is the file.
	racePath := filepath.Join(dir, "race.txt")
	const n = 12
	type result struct {
		i   int
		err error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- result{i: i, err: creator.CreateNewWithContent(ctx, racePath, []byte(fmt.Sprintf("winner-%d", i)), hostos.WriteOptions{})}
		}(i)
	}
	wg.Wait()
	close(results)
	wins := 0
	winner := -1
	for r := range results {
		switch {
		case r.err == nil:
			wins++
			winner = r.i
		case errors.Is(r.err, fs.ErrExist):
		default:
			t.Fatalf("concurrent creator error = %v", r.err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent creators: %d wins, want exactly one", wins)
	}
	if got, err := os.ReadFile(racePath); err != nil || string(got) != fmt.Sprintf("winner-%d", winner) {
		t.Fatalf("winner content = %q, %v; want winner-%d", got, err, winner)
	}
}

// TestLocalCreateNewWithContent checks the local single-op exclusive content creator.
func TestLocalCreateNewWithContent(t *testing.T) {
	dir := t.TempDir()
	slot := authority.Local(authority.LocalOptions{})
	defer func() { _ = slot.Close() }()
	creator := authority.ContentCreators(slot.Files())
	if creator == nil {
		t.Fatal("local backend has no FileContentCreator")
	}
	p := filepath.Join(dir, "a.txt")
	data := []byte("one\x00two\n")
	if err := creator.CreateNewWithContent(context.Background(), p, data, hostos.WriteOptions{Mode: 0o600}); err != nil {
		t.Fatalf("local CreateNewWithContent: %v", err)
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != string(data) {
		t.Fatalf("local content = %q, %v", got, err)
	}
	if err := creator.CreateNewWithContent(context.Background(), p, []byte("theirs"), hostos.WriteOptions{}); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("local create over existing = %v, want fs.ErrExist", err)
	}
}

// TestUnsupportedFilesHasNoContentCreator checks the content-capable surface is distinct
// from FileCreator and absent on a backend that cannot claim+write.
func TestUnsupportedFilesHasNoContentCreator(t *testing.T) {
	if creator := authority.ContentCreators(authority.UnsupportedFiles{}); creator != nil {
		t.Fatal("UnsupportedFiles unexpectedly implements FileContentCreator")
	}
}

// TestSSHCreateNewWithContentWriteFailureKeepsClaim checks a failed content write is not
// turned into a conflict and never deletes the claimed path: the helper writes through the
// same noclobber descriptor it claimed, so the file it created stays (possibly empty) and
// the error is an ordinary helper/write failure.
func TestSSHCreateNewWithContentWriteFailureKeepsClaim(t *testing.T) {
	dir := t.TempDir()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "cat"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creator := authority.ContentCreators(a.Files())
	if creator == nil {
		t.Fatal("the SSH file backend does not implement FileContentCreator")
	}
	p := filepath.Join(dir, "claimed.toml")
	err := creator.CreateNewWithContent(ctx, p, []byte("data"), hostos.WriteOptions{})
	if err == nil {
		t.Fatal("a failing cat reported success")
	}
	if errors.Is(err, fs.ErrExist) {
		t.Fatalf("a write failure was misreported as an existing entry: %v", err)
	}
	if _, statErr := os.Stat(p); statErr != nil {
		t.Fatalf("the claimed file was removed after a write failure: %v", statErr)
	}
}

// TestSSHCreateNewWithContentSymlinkRaceKeepsCanary races a symlink replacement against
// the single claim-and-write helper: if the target is a symlink at claim time it is
// refused; if it is swapped to a symlink after the claim, the helper writes through its
// already-open descriptor, so the symlink's target (the canary) is never modified.
func TestSSHCreateNewWithContentSymlinkRaceKeepsCanary(t *testing.T) {
	dir := t.TempDir()
	canary := filepath.Join(t.TempDir(), "canary")
	if err := os.WriteFile(canary, []byte("safe"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	creator := authority.ContentCreators(a.Files())
	if creator == nil {
		t.Fatal("the SSH file backend does not implement FileContentCreator")
	}
	for i := 0; i < 12; i++ {
		target := filepath.Join(dir, fmt.Sprintf("race-%d.toml", i))
		stop := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = os.Remove(target)
				_ = os.Symlink(canary, target)
			}
		}()
		err := creator.CreateNewWithContent(ctx, target, []byte("payload"), hostos.WriteOptions{})
		close(stop)
		wg.Wait()
		// The racer may create or remove the target between the claim attempt and the
		// helper's existence check, so either success or a helper/conflict error is
		// valid; the security property under test is that the symlink target is never
		// written through.
		_ = err
		if got, readErr := os.ReadFile(canary); readErr != nil || string(got) != "safe" {
			t.Fatalf("iteration %d modified the symlink target: %q, %v", i, got, readErr)
		}
	}
}
