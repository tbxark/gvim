package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig points the user config dir at a fresh temporary home on every
// platform that os.UserConfigDir consults, so tests never touch the real one.
func isolateConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", home)
	return home
}

func resolve(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOpenAcceptsDirectory(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open(dir) error = %v", err)
	}
	if got, want := r.Path(), resolve(t, dir); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestOpenFollowsSymlinkToDirectory(t *testing.T) {
	isolateConfig(t)
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	r, err := Open(link)
	if err != nil {
		t.Fatalf("Open(link) error = %v", err)
	}
	if got, want := r.Path(), resolve(t, target); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestOpenRejectsFile(t *testing.T) {
	isolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Open(file) error = %v, want ErrNotDirectory", err)
	}
	if !strings.HasPrefix(err.Error(), "not a directory: ") {
		t.Fatalf("message = %q, want prefix %q", err.Error(), "not a directory: ")
	}
	if r.Path() != "" {
		t.Fatalf("Open(file) returned root %q, want zero Root", r.Path())
	}
}

func TestOpenRejectsMissingPath(t *testing.T) {
	isolateConfig(t)
	if _, err := Open(filepath.Join(t.TempDir(), "missing")); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Open(missing) error = %v, want ErrNotDirectory", err)
	}
}

func TestOpenRejectsPathBelowFile(t *testing.T) {
	isolateConfig(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "child")); !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Open(file/child) error = %v, want ErrNotDirectory", err)
	}
}

func TestOpenDoesNotRecord(t *testing.T) {
	isolateConfig(t)
	if _, err := Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 0 {
		t.Fatalf("Recent() after Open = %d entries, want 0", len(roots))
	}
}

func TestRecentIsEmptyWithoutFile(t *testing.T) {
	isolateConfig(t)
	roots, err := Recent()
	if err != nil {
		t.Fatalf("Recent() error = %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("Recent() = %d entries, want 0", len(roots))
	}
}

func TestSwitchRecordsMostRecentFirstWithoutDuplicates(t *testing.T) {
	isolateConfig(t)
	base := t.TempDir()
	a := mkdir(t, base, "a")
	b := mkdir(t, base, "b")
	for _, p := range []string{a, b, a} {
		if _, err := Switch(p); err != nil {
			t.Fatalf("Switch(%s) error = %v", p, err)
		}
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{resolve(t, a), resolve(t, b)}
	if len(roots) != len(want) {
		t.Fatalf("Recent() = %d entries, want %d", len(roots), len(want))
	}
	for i := range want {
		if roots[i].Path() != want[i] {
			t.Fatalf("Recent()[%d] = %q, want %q", i, roots[i].Path(), want[i])
		}
	}
}

func TestSwitchCapsRecentList(t *testing.T) {
	isolateConfig(t)
	base := t.TempDir()
	var last string
	for i := range MaxRecent + 3 {
		last = mkdir(t, base, fmt.Sprintf("d%02d", i))
		if _, err := Switch(last); err != nil {
			t.Fatal(err)
		}
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != MaxRecent {
		t.Fatalf("Recent() = %d entries, want %d", len(roots), MaxRecent)
	}
	if roots[0].Path() != resolve(t, last) {
		t.Fatalf("Recent()[0] = %q, want the last switched root", roots[0].Path())
	}
}

func TestRecentSkipsRemovedDirectories(t *testing.T) {
	isolateConfig(t)
	base := t.TempDir()
	a := mkdir(t, base, "a")
	b := mkdir(t, base, "b")
	for _, p := range []string{a, b} {
		if _, err := Switch(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Path() != resolve(t, a) {
		t.Fatalf("Recent() = %v, want only %q", roots, a)
	}
}

func TestSwitchRejectsFileWithoutRecording(t *testing.T) {
	isolateConfig(t)
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Switch(file)
	if !errors.Is(err, ErrNotDirectory) {
		t.Fatalf("Switch(file) error = %v, want ErrNotDirectory", err)
	}
	if r.Path() != "" {
		t.Fatalf("Switch(file) root = %q, want zero Root", r.Path())
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 0 {
		t.Fatalf("Recent() = %d entries after rejected switch, want 0", len(roots))
	}
}

func TestRecentFileLivesInUserConfigDir(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	if _, err := Switch(dir); err != nil {
		t.Fatal(err)
	}
	path, err := recentPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("recent file not written at %s: %v", path, err)
	}
	if !strings.Contains(string(data), resolve(t, dir)) {
		t.Fatalf("recent file = %s, want it to contain %q", data, resolve(t, dir))
	}
}

func TestCorruptRecentListIsReplacedBySwitch(t *testing.T) {
	isolateConfig(t)
	path, err := recentPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Recent(); !errors.Is(err, errCorrupt) {
		t.Fatalf("Recent() on corrupt file error = %v, want errCorrupt", err)
	}
	dir := t.TempDir()
	if _, err := Switch(dir); err != nil {
		t.Fatalf("Switch over corrupt list error = %v", err)
	}
	roots, err := Recent()
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 || roots[0].Path() != resolve(t, dir) {
		t.Fatalf("Recent() = %v, want only %q", roots, dir)
	}
}

func TestSwitchReturnsValidRootWhenRecordFails(t *testing.T) {
	home := isolateConfig(t)
	// Make the config dir unusable: a regular file where a directory is needed.
	blocker := filepath.Join(home, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", blocker)
	t.Setenv("XDG_CONFIG_HOME", blocker)
	t.Setenv("AppData", blocker)

	dir := t.TempDir()
	r, err := Switch(dir)
	if err == nil {
		t.Fatal("Switch with unwritable config dir returned no error")
	}
	if r.Path() != resolve(t, dir) {
		t.Fatalf("Switch root = %q, want %q alongside the save error", r.Path(), resolve(t, dir))
	}
}
