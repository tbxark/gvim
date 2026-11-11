package gitstate_test

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/tbxark/gvim/gitstate"
)

// newRepo creates a repository whose HEAD is branch main. When files is not
// empty it writes them and commits everything in one initial commit.
func newRepo(t *testing.T, files map[string]string) (string, *git.Repository, *git.Worktree) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		DefaultBranch: plumbing.NewBranchReferenceName("main"),
	})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) > 0 {
		for rel, content := range files {
			writeFile(t, dir, rel, content)
		}
		if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
			t.Fatal(err)
		}
		commit(t, wt, "initial")
	}
	return dir, repo, wt
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, wt *git.Worktree, msg string) {
	t.Helper()
	sig := &object.Signature{Name: "tester", Email: "tester@example.com", When: time.Now()}
	if _, err := wt.Commit(msg, &git.CommitOptions{Author: sig}); err != nil {
		t.Fatal(err)
	}
}

func describe(m map[string]gitstate.FileStatus) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(&b, "  %s: %v\n", k, m[k])
	}
	return b.String()
}

func TestFindRoot(t *testing.T) {
	dir, _, _ := newRepo(t, map[string]string{"a/b/file.txt": "x\n"})

	// A linked worktree or submodule keeps .git as a file, which go-git can open.
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /nowhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		dir    string
		want   string
		wantOK bool
	}{
		{name: "repository root", dir: dir, want: dir, wantOK: true},
		{name: "nested directory", dir: filepath.Join(dir, "a", "b"), want: dir, wantOK: true},
		{name: "git file", dir: linked, want: linked, wantOK: true},
		{name: "not a repository", dir: t.TempDir(), want: "", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := gitstate.FindRoot(tt.dir)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("FindRoot(%q) = (%q, %v), want (%q, %v)", tt.dir, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestStatus(t *testing.T) {
	dir, _, wt := newRepo(t, map[string]string{
		".gitignore":      "*.log\n",
		"tracked.txt":     "one\n",
		"clean.txt":       "clean\n",
		"gone.txt":        "gone\n",
		"staged-gone.txt": "bye\n",
		"sub/nested.txt":  "n\n",
	})

	// Work tree edits.
	writeFile(t, dir, "tracked.txt", "one, edited\n")
	writeFile(t, dir, "sub/nested.txt", "n, edited\n")
	if err := os.Remove(filepath.Join(dir, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	// Untracked, and ignored by .gitignore.
	writeFile(t, dir, "new.txt", "new\n")
	writeFile(t, dir, "sub/new-in-sub.txt", "x\n")
	writeFile(t, dir, "debug.log", "noise\n")

	// Staged changes.
	writeFile(t, dir, "staged.txt", "s\n")
	if _, err := wt.Add("staged.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "staged-then-edited.txt", "v1\n")
	if _, err := wt.Add("staged-then-edited.txt"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "staged-then-edited.txt", "v2\n")
	writeFile(t, dir, "staged-then-removed.txt", "v1\n")
	if _, err := wt.Add("staged-then-removed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "staged-then-removed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Remove("staged-gone.txt"); err != nil {
		t.Fatal(err)
	}

	want := map[string]gitstate.FileStatus{
		"tracked.txt":             gitstate.Modified,
		"sub/nested.txt":          gitstate.Modified,
		"gone.txt":                gitstate.Deleted,
		"staged-gone.txt":         gitstate.Deleted,
		"staged-then-removed.txt": gitstate.Deleted,
		"new.txt":                 gitstate.Untracked,
		"sub/new-in-sub.txt":      gitstate.Untracked,
		"staged.txt":              gitstate.Added,
		"staged-then-edited.txt":  gitstate.Added,
	}
	got, err := gitstate.Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("Status() =\n%swant\n%s", describe(got), describe(want))
	}
}

func TestStatusRenamed(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		from  string
		to    string
		// edit, when set, replaces the moved file's content and stages it.
		edit string
		want map[string]gitstate.FileStatus
	}{
		{
			name:  "identical content is a rename",
			files: map[string]string{"old.txt": "same content\n"},
			from:  "old.txt",
			to:    "new.txt",
			want:  map[string]gitstate.FileStatus{"new.txt": gitstate.Renamed},
		},
		{
			name:  "changed content is not a rename",
			files: map[string]string{"old.txt": "same content\n"},
			from:  "old.txt",
			to:    "new.txt",
			edit:  "different content\n",
			want:  map[string]gitstate.FileStatus{"old.txt": gitstate.Deleted, "new.txt": gitstate.Added},
		},
		{
			name:  "empty files are not paired",
			files: map[string]string{"a.txt": ""},
			from:  "a.txt",
			to:    "b.txt",
			want:  map[string]gitstate.FileStatus{"a.txt": gitstate.Deleted, "b.txt": gitstate.Added},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _, wt := newRepo(t, tt.files)
			if _, err := wt.Move(tt.from, tt.to); err != nil {
				t.Fatal(err)
			}
			if tt.edit != "" {
				writeFile(t, dir, tt.to, tt.edit)
				if _, err := wt.Add(tt.to); err != nil {
					t.Fatal(err)
				}
			}
			got, err := gitstate.Status(dir)
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(got, tt.want) {
				t.Fatalf("Status() =\n%swant\n%s", describe(got), describe(tt.want))
			}
		})
	}
}

func TestStatusConflicted(t *testing.T) {
	dir, repo, _ := newRepo(t, map[string]string{"conflict.txt": "base\n"})

	// A merge conflict leaves the path in index stages 1 (base), 2 (ours) and 3
	// (theirs) instead of stage 0.
	idx, err := repo.Storer.Index()
	if err != nil {
		t.Fatal(err)
	}
	var base *index.Entry
	var entries []*index.Entry
	for _, e := range idx.Entries {
		if e.Name == "conflict.txt" {
			base = e
			continue
		}
		entries = append(entries, e)
	}
	if base == nil {
		t.Fatal("conflict.txt is not in the index")
	}
	for _, stage := range []index.Stage{index.AncestorMode, index.OurMode, index.TheirMode} {
		e := *base
		e.Stage = stage
		entries = append(entries, &e)
	}
	slices.SortFunc(entries, func(a, b *index.Entry) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), cmp.Compare(a.Stage, b.Stage))
	})
	idx.Entries = entries
	if err := repo.Storer.SetIndex(idx); err != nil {
		t.Fatal(err)
	}

	want := map[string]gitstate.FileStatus{"conflict.txt": gitstate.Conflicted}
	got, err := gitstate.Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(got, want) {
		t.Fatalf("Status() =\n%swant\n%s", describe(got), describe(want))
	}
}

// TestGitFileWorktree points a .git file at another repository's git dir, the
// shape a linked worktree has. The shared HEAD and index are fine for this
// check: go-git must open the root and read its branch and status.
func TestGitFileWorktree(t *testing.T) {
	main, _, _ := newRepo(t, map[string]string{"a.txt": "a\n"})
	linked := t.TempDir()
	writeFile(t, linked, ".git", "gitdir: "+filepath.Join(main, ".git")+"\n")
	writeFile(t, linked, "a.txt", "a\n")
	writeFile(t, linked, "b.txt", "b\n")

	gotStatus, err := gitstate.Status(linked)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus := map[string]gitstate.FileStatus{"b.txt": gitstate.Untracked}
	if !maps.Equal(gotStatus, wantStatus) {
		t.Fatalf("Status() =\n%swant\n%s", describe(gotStatus), describe(wantStatus))
	}
	gotBranch, err := gitstate.Branch(linked)
	if err != nil {
		t.Fatal(err)
	}
	if gotBranch != "main" {
		t.Fatalf("Branch() = %q, want %q", gotBranch, "main")
	}
}

func TestStatusRequiresWorktreeRoot(t *testing.T) {
	dir, _, _ := newRepo(t, map[string]string{"sub/file.txt": "x\n"})
	if _, err := gitstate.Status(filepath.Join(dir, "sub")); !errors.Is(err, gitstate.ErrNotRepo) {
		t.Fatalf("Status(subdirectory) error = %v, want ErrNotRepo", err)
	}
}

func TestBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch plumbing.ReferenceName
		want   string
	}{
		{name: "default branch", branch: "", want: "main"},
		{name: "slashed branch", branch: plumbing.NewBranchReferenceName("feature/login"), want: "feature/login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, _, wt := newRepo(t, map[string]string{"a.txt": "a\n"})
			if tt.branch != "" {
				if err := wt.Checkout(&git.CheckoutOptions{Branch: tt.branch, Create: true}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := gitstate.Branch(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Branch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBranchUnborn(t *testing.T) {
	dir, _, _ := newRepo(t, nil)
	got, err := gitstate.Branch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Fatalf("Branch() on unborn repository = %q, want %q", got, "main")
	}
}

func TestBranchDetached(t *testing.T) {
	dir, repo, wt := newRepo(t, map[string]string{"a.txt": "a\n"})
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.Checkout(&git.CheckoutOptions{Hash: head.Hash()}); err != nil {
		t.Fatal(err)
	}
	got, err := gitstate.Branch(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := head.Hash().String()[:7]
	if got != want {
		t.Fatalf("Branch() on detached HEAD = %q, want short hash %q", got, want)
	}
}

func TestNotRepo(t *testing.T) {
	plain := t.TempDir()
	if _, err := gitstate.Status(plain); !errors.Is(err, gitstate.ErrNotRepo) {
		t.Fatalf("Status(outside repository) error = %v, want ErrNotRepo", err)
	}
	if _, err := gitstate.Branch(plain); !errors.Is(err, gitstate.ErrNotRepo) {
		t.Fatalf("Branch(outside repository) error = %v, want ErrNotRepo", err)
	}
}
