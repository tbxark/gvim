package finder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// writeFiles creates the files under root; a trailing "/" makes a directory.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if name[len(name)-1] == '/' {
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// collect runs Enumerate and returns the sorted paths it emitted.
func collect(t *testing.T, root string) []string {
	t.Helper()
	var got []string
	err := Enumerate(context.Background(), root, func(rel string) bool {
		got = append(got, rel)
		return true
	})
	if err != nil {
		t.Fatalf("Enumerate(%q): %v", root, err)
	}
	slices.Sort(got)
	return got
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
}

func TestEnumerateGitRepo(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".gitignore":     "*.log\nbin/\n",
		"tracked.go":     "",
		"a.go":           "",
		"b.log":          "",
		"bin/tool":       "",
		"sub/c.go":       "",
		".github/ci.yml": "",
	})
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if out, err := exec.Command("git", "-C", root, "add", "tracked.go").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	want := []string{".github/ci.yml", ".gitignore", "a.go", "sub/c.go", "tracked.go"}
	if got := collect(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("git enumeration = %v\nwant              %v", got, want)
	}

	// A root inside the work tree lists only its own files, relative to it.
	if got := collect(t, filepath.Join(root, "sub")); !reflect.DeepEqual(got, []string{"c.go"}) {
		t.Fatalf("subdirectory enumeration = %v, want [c.go]", got)
	}
}

func TestEnumerateWalkFallbackOnPlainDirectory(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".gitignore":        "*.log\n",
		"a.go":              "",
		"b.log":             "",
		".env":              "",
		"node_modules/x.js": "",
		"sub/c.go":          "",
	})
	want := []string{"a.go", "sub/c.go"}
	if got := collect(t, root); !reflect.DeepEqual(got, want) {
		t.Fatalf("walk enumeration = %v\nwant              %v", got, want)
	}
}

func TestEnumerateStopsWhenEmitReturnsFalse(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a": "", "b": "", "c": ""})
	calls := 0
	if err := Enumerate(context.Background(), root, func(string) bool {
		calls++
		return false
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("emit called %d times, want 1", calls)
	}
}

func TestEnumerateCancelledContext(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a": ""})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Enumerate(ctx, root, func(string) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enumerate with cancelled ctx = %v, want context.Canceled", err)
	}
}

func TestStartFillsIndexInBackground(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.go": "", "sub/b.go": ""})
	x := Start(context.Background(), root)
	defer x.Close()
	<-x.Done()
	files, loading := x.Snapshot()
	slices.Sort(files)
	if loading || x.Loading() {
		t.Fatal("index still loading after Done")
	}
	if !reflect.DeepEqual(files, []string{"a.go", "sub/b.go"}) {
		t.Fatalf("files = %v", files)
	}
	if x.Err() != nil || x.Truncated() {
		t.Fatalf("Err = %v, Truncated = %v", x.Err(), x.Truncated())
	}
}

func TestStartErrorOnMissingRoot(t *testing.T) {
	x := Start(context.Background(), filepath.Join(t.TempDir(), "absent"))
	defer x.Close()
	<-x.Done()
	if x.Err() == nil {
		t.Fatal("missing root: Err is nil")
	}
}

func TestIndexCloseStopsEnumeration(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.go": ""})
	x := Start(context.Background(), root)
	x.Close()
	if x.Loading() {
		t.Fatal("Loading after Close")
	}
}

func TestIndexCapsAtMaxFiles(t *testing.T) {
	var x Index
	for i := range MaxFiles {
		if !x.add(fmt.Sprintf("f%05d", i)) {
			t.Fatalf("add %d refused below the cap", i)
		}
	}
	if x.add("overflow") {
		t.Fatal("add accepted a file past MaxFiles")
	}
	files, _ := x.Snapshot()
	if len(files) != MaxFiles || !x.Truncated() {
		t.Fatalf("len = %d, Truncated = %v", len(files), x.Truncated())
	}
}
