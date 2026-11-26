package uiadapter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/gitstate"
	"github.com/tbxark/gvim/internal/apptest"
)

func TestGitMarkLetters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status gitstate.FileStatus
		want   rune
	}{
		{gitstate.Added, 'A'},
		{gitstate.Modified, 'M'},
		{gitstate.Deleted, 'D'},
		{gitstate.Untracked, 'U'},
		{gitstate.Renamed, 'R'},
		{gitstate.Conflicted, 'C'},
	}
	for _, tt := range tests {
		if got := gitMark(tt.status); got != tt.want {
			t.Errorf("gitMark(%v) = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// TestGitStatusMarksAbsolutePaths checks the adapter on a real repository: an
// untracked file is U and a staged new file is A, both keyed by the absolute path
// of the file, even when the folder read is a subfolder of the work tree.
func TestGitStatusMarksAbsolutePaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-b", "main")
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	apptest.Must(t, os.WriteFile(filepath.Join(sub, "u.txt"), []byte("u"), 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644))
	runGit("add", "a.txt")

	marks, err := GitStatus{}.Marks(sub)
	if err != nil {
		t.Fatalf("Marks() error = %v", err)
	}
	want := map[string]rune{
		filepath.Join(root, "sub", "u.txt"): 'U',
		filepath.Join(root, "a.txt"):        'A',
	}
	for p, w := range want {
		if got := marks[p]; got != w {
			t.Errorf("mark of %s = %q, want %q (all marks %v)", p, got, w, marks)
		}
	}
}

func TestGitStatusOutsideARepositoryIsEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marks, err := GitStatus{}.Marks(dir)
	if err != nil {
		t.Fatalf("Marks() outside a repository error = %v", err)
	}
	if len(marks) != 0 {
		t.Fatalf("Marks() outside a repository = %v, want none", marks)
	}
}
