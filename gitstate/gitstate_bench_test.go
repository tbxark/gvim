package gitstate_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/gitstate"
)

func runGitBench(b *testing.B, dir string, args ...string) {
	b.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func setupBenchRepo(b *testing.B, files int) string {
	b.Helper()
	dir := b.TempDir()
	runGitBench(b, dir, "init", "-b", "main")
	runGitBench(b, dir, "config", "user.name", "bench")
	runGitBench(b, dir, "config", "user.email", "bench@example.com")
	for i := range files {
		p := filepath.Join(dir, fmt.Sprintf("pkg/file_%03d.go", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(p, fmt.Appendf(nil, "package pkg\nconst v%d = %d\n", i, i), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	runGitBench(b, dir, "add", "-A")
	runGitBench(b, dir, "commit", "-m", "initial")
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
