package uiadapter

import (
	"github.com/tbxark/gvim/internal/apptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"

	"github.com/tbxark/gvim/gitstate"
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
	repo, err := git.PlainInit(root, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	apptest.Must(t, os.WriteFile(filepath.Join(sub, "u.txt"), []byte("u"), 0o644))
	apptest.Must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644))
	if _, err := wt.Add("a.txt"); err != nil {
		t.Fatal(err)
	}

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
