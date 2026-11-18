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

// TestEnumerateAllIncludesHiddenAndIgnored: the Ignored scope of Live Grep uses
// EnumerateAll to see the files the plain listing leaves out, and the zero options keep
// the plain listing.
func TestEnumerateAllIncludesHiddenAndIgnored(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".gitignore":     "ignored.txt\n",
		"visible.go":     "",
		".hidden.go":     "",
		"ignored.txt":    "",
		"sub/visible.md": "",
	})
	collectAll := func(opts Options) []string {
		t.Helper()
		var got []string
		err := EnumerateAll(context.Background(), root, opts, func(rel string) bool {
			got = append(got, rel)
			return true
		})
		if err != nil {
			t.Fatalf("EnumerateAll(%+v): %v", opts, err)
		}
		slices.Sort(got)
		return got
	}
	plain := collectAll(Options{})
	if slices.Contains(plain, ".hidden.go") || slices.Contains(plain, "ignored.txt") {
		t.Fatalf("the plain listing includes hidden or ignored files: %v", plain)
	}
	if !slices.Contains(plain, "visible.go") || !slices.Contains(plain, "sub/visible.md") {
		t.Fatalf("the plain listing misses a visible file: %v", plain)
	}
	all := collectAll(Options{ShowHidden: true, ShowIgnored: true})
	for _, want := range []string{".hidden.go", "ignored.txt", ".gitignore", "visible.go"} {
		if !slices.Contains(all, want) {
			t.Errorf("the hidden-and-ignored listing misses %q: %v", want, all)
		}
	}
}

func TestIndexCapsAtMaxFiles(t *testing.T) {
	var x Index
	for i := range MaxFiles {
		if !x.addFor(0, fmt.Sprintf("f%05d", i)) {
			t.Fatalf("addFor %d refused below the cap", i)
		}
	}
	if x.addFor(0, "overflow") {
		t.Fatal("addFor accepted a file past MaxFiles")
	}
	files, _ := x.Snapshot()
	if len(files) != MaxFiles || !x.Truncated() {
		t.Fatalf("len = %d, Truncated = %v", len(files), x.Truncated())
	}
}

func TestEnumerateCustomIgnorePatterns(t *testing.T) {
	// 1. In a git repository
	requireGit(t)
	gitRoot := t.TempDir()
	writeFiles(t, gitRoot, map[string]string{
		".gitignore":  "gitignored.txt\n",
		"tracked.txt": "hello",
		"app.log":     "log",
		"build/out":   "binary",
		"sub/deep.log": "deep log",
		"sub/keep.txt": "keep",
	})
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", gitRoot}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.name", "test")
	run("config", "user.email", "test@test.local")
	run("add", ".")
	run("commit", "-m", "init")

	// Custom ignore patterns should filter app.log, build/out, sub/deep.log even from git enumeration
	var gitFiltered []string
	err := EnumerateWithOptions(context.Background(), gitRoot, Options{
		CustomIgnorePatterns: []string{"*.log", "build/"},
	}, func(rel string) bool {
		gitFiltered = append(gitFiltered, rel)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(gitFiltered)
	wantGit := []string{".gitignore", "sub/keep.txt", "tracked.txt"}
	if !reflect.DeepEqual(gitFiltered, wantGit) {
		t.Fatalf("gitFiltered = %v, want %v", gitFiltered, wantGit)
	}

	// 2. In a plain directory (walk fallback)
	plainRoot := t.TempDir()
	writeFiles(t, plainRoot, map[string]string{
		".gitignore":     "gitignored.txt\n",
		"gitignored.txt": "ignored by gitignore",
		"app.log":        "log",
		"build/out":      "binary",
		"normal.txt":     "normal",
	})

	respectFalse := false
	var walkFiltered []string
	err = EnumerateWithOptions(context.Background(), plainRoot, Options{
		RespectGitignore:     &respectFalse,
		CustomIgnorePatterns: []string{"*.log", "build/"},
	}, func(rel string) bool {
		walkFiltered = append(walkFiltered, rel)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(walkFiltered)
	// Because RespectGitignore is false, gitignored.txt is included, but *.log and build/ are excluded
	wantWalk := []string{"gitignored.txt", "normal.txt"}
	if !reflect.DeepEqual(walkFiltered, wantWalk) {
		t.Fatalf("walkFiltered = %v, want %v", walkFiltered, wantWalk)
	}

	// 3. Test Index.SetOptions re-enumeration
	idx := Start(context.Background(), gitRoot, Options{
		CustomIgnorePatterns: []string{"*.log", "build/"},
	})
	defer idx.Close()
	// Wait until loaded
	for {
		_, loading := idx.Snapshot()
		if !loading {
			break
		}
	}
	files, _ := idx.Snapshot()
	slices.Sort(files)
	if !reflect.DeepEqual(files, wantGit) {
		t.Fatalf("index files = %v, want %v", files, wantGit)
	}

	// Re-enumerate with no ignore patterns
	idx.SetOptions(gitRoot, Options{})
	for {
		_, loading := idx.Snapshot()
		if !loading {
			break
		}
	}
	files2, _ := idx.Snapshot()
	if !slices.Contains(files2, "app.log") {
		t.Fatalf("expected app.log after SetOptions without custom patterns, got %v", files2)
	}
}

func TestIndexProbeShowHiddenAndIdleClearOnSetOptions(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"sub/.secret.go": "package sub",
		"sub/visible.go": "package sub",
		"main.go":        "package main",
	})

	idx := StartWithOptions(context.Background(), root, Options{ShowHidden: true})
	defer idx.Close()
	<-idx.Done()
	got, err := idx.Probe(context.Background(), "sub")
	if err != nil || !slices.Contains(got, "sub/.secret.go") {
		t.Fatalf("StartWithOptions(ShowHidden: true) probe = %v, %v; want sub/.secret.go", got, err)
	}

	idx.SetOptions(root, Options{ShowHidden: false})
	<-idx.Done()
	got2, err := idx.Probe(context.Background(), "sub")
	if err != nil || slices.Contains(got2, "sub/.secret.go") || !slices.Contains(got2, "sub/visible.go") {
		t.Fatalf("SetOptions(ShowHidden: false) probe = %v, %v; want only sub/visible.go", got2, err)
	}

	idle := Idle()
	defer idle.Close()
	idle.SetOptions(root, Options{})
	<-idle.Done()
	files, loading, ready, isIdle, _, _, _, err := idle.IndexSnapshot()
	if err != nil || loading || isIdle || !ready || !slices.Contains(files, "main.go") {
		t.Fatalf("after Idle.SetOptions: files=%v loading=%v ready=%v idle=%v err=%v", files, loading, ready, isIdle, err)
	}
}
