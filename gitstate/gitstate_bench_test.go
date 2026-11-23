package gitstate_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/tbxark/gvim/gitstate"
)

func setupBenchRepo(b *testing.B, files int) string {
	b.Helper()
	dir := b.TempDir()
	repo, err := git.PlainInitWithOptions(dir, &git.PlainInitOptions{
		DefaultBranch: plumbing.NewBranchReferenceName("main"),
	})
	if err != nil {
		b.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		b.Fatal(err)
	}
	for i := range files {
		p := filepath.Join(dir, fmt.Sprintf("pkg/file_%03d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, fmt.Appendf(nil, "package pkg\nconst v%d = %d\n", i, i), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	if err := wt.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		b.Fatal(err)
	}
	sig := &object.Signature{Name: "bench", Email: "bench@example.com", When: time.Now()}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: sig}); err != nil {
		b.Fatal(err)
	}
	return dir
}

func BenchmarkBranch(b *testing.B) {
	dir := setupBenchRepo(b, 5)
	b.ReportAllocs()
	for b.Loop() {
		br, err := gitstate.Branch(dir)
		if err != nil || br != "main" {
			b.Fatalf("Branch() = %q, %v", br, err)
		}
	}
}

func BenchmarkStatus(b *testing.B) {
	dir := setupBenchRepo(b, 50)
	if err := os.WriteFile(filepath.Join(dir, "pkg/file_000.go"), []byte("package pkg\nconst v0 = 999\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package main\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		st, err := gitstate.Status(dir)
		if err != nil || len(st) != 2 {
			b.Fatalf("Status() = %v, %v", st, err)
		}
	}
}
