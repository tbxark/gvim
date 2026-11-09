package finder

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestProbePathRejectsEscapes(t *testing.T) {
	valid := []string{"a.go", "dir/a.go", "my file.txt", "a:b.go", "dir/../b.go"}
	for _, q := range valid {
		if _, ok := ProbePath(q); !ok {
			t.Errorf("ProbePath(%q) rejected a valid relative path", q)
		}
	}
	invalid := []string{"", ".", "..", "../a.go", "dir/../../a.go", "/etc/hosts", `C:\x\a.go`, string([]byte{0}) + "a"}
	for _, q := range invalid {
		if _, ok := ProbePath(q); ok {
			t.Errorf("ProbePath(%q) accepted an escaping or absolute path", q)
		}
	}
	if got, ok := ProbePath("dir/../b.go"); !ok || got != "b.go" {
		t.Fatalf("ProbePath cleaned = %q, %v; want b.go, true", got, ok)
	}
}

func TestLocalPrefixProbeFindsExactFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.go", "my file.txt", "sub/b.go"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	probe := LocalPrefixProbe(root)
	for _, tt := range []struct {
		query string
		want  []string
	}{
		{"a.go", []string{"a.go"}},
		{"sub/b.go", []string{"sub/b.go"}},
		{"my file.txt", []string{"my file.txt"}},
		{"missing.go", nil},
		{"sub", nil}, // a directory is not a file result
	} {
		got, err := probe(context.Background(), tt.query)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("probe(%q) = %v, %v; want %v, nil", tt.query, got, err, tt.want)
		}
	}
}

func TestLocalPrefixProbeRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "outdir")); err != nil {
		t.Fatal(err)
	}
	probe := LocalPrefixProbe(root)
	for _, query := range []string{"link.go", "outdir/secret.go"} {
		got, err := probe(context.Background(), query)
		if err != nil || len(got) != 0 {
			t.Errorf("probe(%q) = %v, %v; want no result for a symlink escape", query, got, err)
		}
	}
	// A symlink whose target is still inside the workspace is contained.
	inside := filepath.Join(root, "inside.go")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inside, filepath.Join(root, "link-inside.go")); err != nil {
		t.Fatal(err)
	}
	if got, err := probe(context.Background(), "link-inside.go"); err != nil || len(got) != 1 || got[0] != "link-inside.go" {
		t.Errorf("contained symlink probe = %v, %v; want link-inside.go", got, err)
	}
}

func TestLocalPrefixProbeThroughSymlinkRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	realRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(realRoot, "a.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, rootLink); err != nil {
		t.Fatal(err)
	}
	if got, err := LocalPrefixProbe(rootLink)(context.Background(), "a.go"); err != nil || !reflect.DeepEqual(got, []string{"a.go"}) {
		t.Fatalf("symlink-root probe = %v, %v; want a.go", got, err)
	}
}

func TestLocalPrefixProbeHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LocalPrefixProbe(root)(ctx, "a.go"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe error = %v, want context.Canceled", err)
	}
}
