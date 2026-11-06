package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// chdirTemp changes into a fresh directory for the rest of the test and returns
// its resolved path. The path is resolved so that a relative name and the
// absolute path of the same file agree, as they do on macOS, where the temporary
// directory is behind a symbolic link. The test must not be parallel.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir
}

func TestRelativePathsAreStoredAbsolute(t *testing.T) {
	dir := chdirTemp(t)
	putFile(t, filepath.Join(dir, "a.txt"), "a")
	tests := []struct {
		name string
		run  func(e *Editor) error
		want string
	}{
		{"OpenFile", func(e *Editor) error { return e.OpenFile("a.txt") }, filepath.Join(dir, "a.txt")},
		{"OpenFile new file", func(e *Editor) error { return e.OpenFile("new.txt") }, filepath.Join(dir, "new.txt")},
		{"OpenPreview", func(e *Editor) error { return e.OpenPreview("a.txt") }, filepath.Join(dir, "a.txt")},
		{"SaveAs", func(e *Editor) error { return e.SaveAs("b.txt") }, filepath.Join(dir, "b.txt")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(Options{})
			if err := tt.run(e); err != nil {
				t.Fatal(err)
			}
			if got := e.Active().Path; got != tt.want {
				t.Errorf("Path = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelativeOpenPathFollowsRenameAndSave(t *testing.T) {
	dir := chdirTemp(t)
	from := filepath.Join(dir, "a.txt")
	to := filepath.Join(dir, "b.txt")
	putFile(t, from, "one\n")
	e := New(Options{})
	if err := e.OpenFile("a.txt"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	typeText(t, e, "x ")
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(from, to); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if got := e.Active().Path; got != to {
		t.Fatalf("Path = %q, want the moved %q", got, to)
	}
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readFile(t, to); got != "x one\n" {
		t.Fatalf("file = %q, want the buffer text", got)
	}
	if _, err := os.Stat(from); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Save recreated the old name: stat err = %v", err)
	}
}

func TestRelativeOpenPathClosesByAbsolutePath(t *testing.T) {
	dir := chdirTemp(t)
	a := filepath.Join(dir, "a.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile("a.txt"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := e.CloseByPath(a, false); err != nil {
		t.Fatalf("CloseByPath: %v", err)
	}
	if !e.Empty() {
		t.Fatalf("CloseByPath left tabs %+v", e.Tabs())
	}
}

func TestCheckRenameAgreesWithRenamePathForRelativeOpen(t *testing.T) {
	dir := chdirTemp(t)
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := New(Options{})
	if err := e.OpenFile("a.txt"); err != nil {
		t.Fatalf("OpenFile a: %v", err)
	}
	if err := e.OpenFile("b.txt"); err != nil {
		t.Fatalf("OpenFile b: %v", err)
	}
	if check := e.CheckRename(a, b); !errors.Is(check, ErrAlreadyOpen) {
		t.Fatalf("CheckRename: err = %v, want ErrAlreadyOpen", check)
	}
	if rename := e.RenamePath(a, b); !errors.Is(rename, ErrAlreadyOpen) {
		t.Fatalf("RenamePath: err = %v, want ErrAlreadyOpen", rename)
	}
}

// dotDotAfterLink builds a directory with a file at dir/file.txt, a file at
// dir/real/file.txt, and dir/link pointing at dir/real/sub. It chdirs into dir.
// The spelling "link/../file.txt" names dir/real/file.txt for the kernel, while
// a lexical clean gives dir/file.txt.
func dotDotAfterLink(t *testing.T) {
	t.Helper()
	dir := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(dir, "real", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(dir, "real", "file.txt"), "kernel\n")
	putFile(t, filepath.Join(dir, "file.txt"), "lexical\n")
	if err := os.Symlink(filepath.Join(dir, "real", "sub"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
}

func TestOpenDotDotAfterSymlinkNamesKernelFile(t *testing.T) {
	dotDotAfterLink(t)
	want, err := filepath.EvalSymlinks("link/../file.txt")
	if err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile("link/../file.txt"); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if got := e.Active().Path; got != want {
		t.Fatalf("Path = %q, want %q (the file the kernel opens)", got, want)
	}
	if got := e.Active().Buffer.Text(); got != "kernel\n" {
		t.Fatalf("text = %q, want the kernel's file", got)
	}
}

func TestSaveAsDotDotAfterSymlinkWritesKernelFile(t *testing.T) {
	dotDotAfterLink(t)
	realDir, err := filepath.EvalSymlinks("link/..")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(realDir, "new.txt")
	e := New(Options{})
	typeText(t, e, "draft")
	if err := e.SaveAs("link/../new.txt"); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := e.Active().Path; got != want {
		t.Fatalf("Path = %q, want %q (the file the kernel writes)", got, want)
	}
	if got := readFile(t, want); got != "draft" {
		t.Fatalf("file = %q, want the buffer text", got)
	}
}

// TestDotDotInOneDirectoryKeepsTheCallersSpelling is the case the round-5 review
// found. The directory is spelled without resolving it, which on macOS goes
// through the /var link. "dir/sub/../f.txt" names the same file as "dir/f.txt",
// and the explorer lists the tree under the spelling the caller used, so the
// stored path must keep that spelling. Resolving the directory physically would
// store a different string for the same file, and a rename would then miss the tab.
func TestDotDotInOneDirectoryKeepsTheCallersSpelling(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	spelled := root + sep + "sub" + sep + ".." + sep + "f.txt"
	got, err := absPath(spelled)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "f.txt"); got != want {
		t.Errorf("absPath(%q) = %q, want %q: the spelling of the directory must be kept", spelled, got, want)
	}
}
