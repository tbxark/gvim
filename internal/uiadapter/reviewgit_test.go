package uiadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/tui/reviewdiff"
)

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func writeTestFile(t *testing.T, dir, rel string, content []byte) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestReviewGitNotRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	res, err := (ReviewGit{}).Changes(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatalf("Changes err = %v, want nil", err)
	}
	if !res.NotRepo {
		t.Fatalf("NotRepo = false, want true")
	}
}

func TestReviewGitChangesDiffAndBaseRef(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	runGitTest(t, dir, "init", "-b", "main")

	writeTestFile(t, dir, "keep.txt", []byte("one\ntwo\nthree\n"))
	writeTestFile(t, dir, "del.txt", []byte("bye\n"))
	writeTestFile(t, dir, "oldname.txt", []byte("rename me line 1\nrename me line 2\nrename me line 3\nline4\n"))
	runGitTest(t, dir, "add", ".")
	runGitTest(t, dir, "commit", "-m", "initial")

	// Create a feature branch with one commit, then working-tree edits.
	runGitTest(t, dir, "checkout", "-b", "feat")
	writeTestFile(t, dir, "branch_only.txt", []byte("from feat\n"))
	runGitTest(t, dir, "add", "branch_only.txt")
	runGitTest(t, dir, "commit", "-m", "feat commit")

	// Working tree changes: modified (staged+unstaged), deleted, renamed, untracked, binary.
	writeTestFile(t, dir, "keep.txt", []byte("one\nTWO\nthree\nfour\n"))
	runGitTest(t, dir, "add", "keep.txt")
	writeTestFile(t, dir, "keep.txt", []byte("one\nTWO\nthree\nfour\nfive\n"))
	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, dir, "mv", "oldname.txt", "newname.txt")
	writeTestFile(t, dir, "newname.txt", []byte("rename me line 1\nrename me line 2\nrename me line 3\nline4 edited\n"))
	writeTestFile(t, dir, "notes.md", []byte("# hello\nworld\n"))
	writeTestFile(t, dir, "blob.bin", []byte{0x00, 0x01, 0x02, 0x03})

	rg := ReviewGit{}
	ctx := context.Background()

	// 1. Default vs HEAD: every file appears once in flat list.
	res, err := rg.Changes(ctx, dir, "HEAD")
	if err != nil {
		t.Fatalf("Changes(HEAD): %v", err)
	}
	if res.NotRepo || res.Branch != "feat" {
		t.Fatalf("res = %+v, want NotRepo=false Branch=feat", res)
	}
	byPath := map[string]reviewdiff.FileEntry{}
	for _, f := range res.Files {
		if _, dup := byPath[f.Path]; dup {
			t.Fatalf("duplicate path %q in flat changes list", f.Path)
		}
		byPath[f.Path] = f
	}
	if f, ok := byPath["keep.txt"]; !ok || f.Status != reviewdiff.StatusModified || f.Added != 3 || f.Deleted != 1 {
		t.Fatalf("keep.txt = %+v (%v), want Modified +3 -1", f, ok)
	}
	if f, ok := byPath["del.txt"]; !ok || f.Status != reviewdiff.StatusDeleted {
		t.Fatalf("del.txt = %+v (%v), want Deleted", f, ok)
	}
	if f, ok := byPath["newname.txt"]; !ok || f.Status != reviewdiff.StatusRenamed || f.OldPath != "oldname.txt" {
		t.Fatalf("newname.txt = %+v (%v), want Renamed from oldname.txt", f, ok)
	}
	if f, ok := byPath["notes.md"]; !ok || f.Status != reviewdiff.StatusUntracked || f.Added != 2 {
		t.Fatalf("notes.md = %+v (%v), want Untracked +2", f, ok)
	}
	if f, ok := byPath["blob.bin"]; !ok || !f.Binary {
		t.Fatalf("blob.bin = %+v (%v), want Binary=true", f, ok)
	}

	// 2. Load FileDiff for modified, renamed, untracked, binary.
	fd, err := rg.FileDiff(ctx, res.RepoRoot, "HEAD", byPath["keep.txt"])
	if err != nil {
		t.Fatalf("FileDiff(keep.txt): %v", err)
	}
	if len(fd.Hunks) == 0 || len(fd.OldLines) != 3 || len(fd.NewLines) != 5 {
		t.Fatalf("keep.txt diff = old:%d new:%d hunks:%d", len(fd.OldLines), len(fd.NewLines), len(fd.Hunks))
	}
	rfd, err := rg.FileDiff(ctx, res.RepoRoot, "HEAD", byPath["newname.txt"])
	if err != nil {
		t.Fatalf("FileDiff(newname.txt): %v", err)
	}
	if len(rfd.OldLines) != 4 || len(rfd.NewLines) != 4 || len(rfd.Hunks) != 1 {
		t.Fatalf("rename diff = old:%d new:%d hunks:%d", len(rfd.OldLines), len(rfd.NewLines), len(rfd.Hunks))
	}
	bfd, err := rg.FileDiff(ctx, res.RepoRoot, "HEAD", byPath["blob.bin"])
	if err != nil || !bfd.Binary {
		t.Fatalf("binary diff = %+v, err=%v", bfd, err)
	}

	// 3. Switching base ref to main includes branch_only.txt as well.
	mainRes, err := rg.Changes(ctx, dir, "main")
	if err != nil {
		t.Fatalf("Changes(main): %v", err)
	}
	foundBranchFile := false
	for _, f := range mainRes.Files {
		if f.Path == "branch_only.txt" && f.Status == reviewdiff.StatusAdded {
			foundBranchFile = true
		}
	}
	if !foundBranchFile {
		t.Fatalf("Changes(main) missing branch_only.txt: %+v", mainRes.Files)
	}

	// 4. Refs includes HEAD, main, and feat.
	refs, err := rg.Refs(ctx, dir)
	if err != nil {
		t.Fatalf("Refs: %v", err)
	}
	for _, want := range []string{"HEAD", "main", "feat"} {
		if !slices.Contains(refs, want) {
			t.Errorf("Refs = %v, missing %q", refs, want)
		}
	}
}
