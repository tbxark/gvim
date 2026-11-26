package gitstate_test

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/gitstate"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a repository whose HEAD is branch main. When files is not
// empty it writes them and commits everything in one initial commit.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.name", "tester")
	runGit(t, dir, "config", "user.email", "tester@example.com")
	if len(files) > 0 {
		for rel, content := range files {
			writeFile(t, dir, rel, content)
		}
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-m", "initial")
	}
	return dir
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

func describe(m map[string]gitstate.FileStatus) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(m)) {
		fmt.Fprintf(&b, "  %s: %v\n", k, m[k])
	}
	return b.String()
}

func TestFindRoot(t *testing.T) {
	dir := newRepo(t, map[string]string{"a/b/file.txt": "x\n"})

	// A linked worktree or submodule keeps .git as a file.
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
	dir := newRepo(t, map[string]string{
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
	runGit(t, dir, "add", "staged.txt")
	writeFile(t, dir, "staged-then-edited.txt", "v1\n")
	runGit(t, dir, "add", "staged-then-edited.txt")
	writeFile(t, dir, "staged-then-edited.txt", "v2\n")
	writeFile(t, dir, "staged-then-removed.txt", "v1\n")
	runGit(t, dir, "add", "staged-then-removed.txt")
	if err := os.Remove(filepath.Join(dir, "staged-then-removed.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "rm", "staged-gone.txt")

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
		edit  string
		want  map[string]gitstate.FileStatus
	}{
		{
			name:  "identical content is a rename",
			files: map[string]string{"old.txt": "same content\n"},
			from:  "old.txt",
			to:    "new.txt",
			want:  map[string]gitstate.FileStatus{"new.txt": gitstate.Renamed},
		},
		{
			name:  "completely different content is not a rename",
			files: map[string]string{"old.txt": "alpha beta gamma delta\n"},
			from:  "old.txt",
			to:    "new.txt",
			edit:  "12345 67890 totally unrelated text here\n",
			want:  map[string]gitstate.FileStatus{"old.txt": gitstate.Deleted, "new.txt": gitstate.Added},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newRepo(t, tt.files)
			runGit(t, dir, "mv", tt.from, tt.to)
			if tt.edit != "" {
				writeFile(t, dir, tt.to, tt.edit)
				runGit(t, dir, "add", tt.to)
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
	dir := newRepo(t, map[string]string{"conflict.txt": "base\n"})
	runGit(t, dir, "checkout", "-b", "other")
	writeFile(t, dir, "conflict.txt", "theirs\n")
	runGit(t, dir, "commit", "-am", "theirs")

	runGit(t, dir, "checkout", "main")
	writeFile(t, dir, "conflict.txt", "ours\n")
	runGit(t, dir, "commit", "-am", "ours")

	cmd := exec.Command("git", "merge", "other")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		t.Fatal("expected merge conflict")
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

func TestGitFileWorktree(t *testing.T) {
	main := newRepo(t, map[string]string{"a.txt": "a\n"})
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
	dir := newRepo(t, map[string]string{"sub/file.txt": "x\n"})
	if _, err := gitstate.Status(filepath.Join(dir, "sub")); !errors.Is(err, gitstate.ErrNotRepo) {
		t.Fatalf("Status(subdirectory) error = %v, want ErrNotRepo", err)
	}
}

func TestBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   string
	}{
		{name: "default branch", branch: "", want: "main"},
		{name: "slashed branch", branch: "feature/login", want: "feature/login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := newRepo(t, map[string]string{"a.txt": "a\n"})
			if tt.branch != "" {
				runGit(t, dir, "checkout", "-b", tt.branch)
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
	dir := newRepo(t, nil)
	got, err := gitstate.Branch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "main" {
		t.Fatalf("Branch() on unborn repository = %q, want %q", got, "main")
	}
}

func TestBranchDetached(t *testing.T) {
	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	hash := runGit(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "checkout", "--detach", hash)
	got, err := gitstate.Branch(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := hash[:7]
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

func TestLinkedWorktreeCommonDir(t *testing.T) {
	mainDir := newRepo(t, map[string]string{"tracked.txt": "base\n"})
	wtGitDir := filepath.Join(mainDir, ".git", "worktrees", "wt1")
	if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wtDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "gitdir"), []byte(filepath.Join(wtDir, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: "+wtGitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, wtDir, "untracked.txt", "hi\n")
	branch, err := gitstate.Branch(wtDir)
	if err != nil || branch != "main" {
		t.Fatalf("Branch(linked worktree) = %q, %v; want main", branch, err)
	}
	st, err := gitstate.Status(wtDir)
	if err != nil {
		t.Fatalf("Status(linked worktree) = %v", err)
	}
	if st["untracked.txt"] != gitstate.Untracked {
		t.Fatalf("Status = %+v, want untracked.txt Untracked", st)
	}
}

func TestBranchGitFileRelativeAndCustomSymref(t *testing.T) {
	base := t.TempDir()
	repoDir := filepath.Join(base, "mainrepo")
	wtDir := filepath.Join(base, "wt")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repoDir, "init", "-b", "main")
	wtGitDir := filepath.Join(repoDir, ".git", "worktrees", "wt1")
	if err := os.MkdirAll(wtGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtGitDir, "HEAD"), []byte("ref: refs/heads/feat/fast\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, ".git"), []byte("gitdir: ../mainrepo/.git/worktrees/wt1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gitstate.Branch(wtDir)
	if err != nil || got != "feat/fast" {
		t.Fatalf("Branch(relative gitdir) = %q, %v; want feat/fast", got, err)
	}

	dir := newRepo(t, map[string]string{"a.txt": "a\n"})
	headPath := filepath.Join(dir, ".git", "HEAD")
	if err := os.WriteFile(headPath, []byte("ref: OTHER\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotFallback, err := gitstate.Branch(dir)
	if err != nil || gotFallback != "OTHER" {
		t.Fatalf("Branch(non-refs symref) = %q, %v; want OTHER", gotFallback, err)
	}
}
