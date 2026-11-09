package finder

import (
	"context"
	"errors"
	"fmt"
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
	for _, name := range []string{"a.go", "a2.go", "save_file.rs", "save_other.rs", ".hidden", "my file.txt", "sub/b.go", "sub/.dot.go"} {
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
		{"a", []string{"a.go", "a2.go"}}, // parent basename prefix matches
		{"save", []string{"save_file.rs", "save_other.rs"}},
		{".hidden", []string{".hidden"}}, // an explicitly hidden basename is included
		{"sub/b.go", []string{"sub/b.go"}},
		{"my file.txt", []string{"my file.txt"}},
		{"missing.go", nil},
		{"sub", []string{"sub/b.go"}}, // a directory query lists its visible files
		{"save_missing", nil},
		{"other", nil},
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

// TestLocalPrefixProbeResultLimit: a directory with more prefix matches than
// ProbeLimit returns at most ProbeLimit, in sorted order.
func TestLocalPrefixProbeResultLimit(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < ProbeLimit+10; i++ {
		if err := os.WriteFile(filepath.Join(root, "f"+fmt.Sprintf("%03d", i)+".go"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := LocalPrefixProbe(root)(context.Background(), "f")
	if err != nil || len(got) != ProbeLimit {
		t.Fatalf("prefix probe = %d paths, %v; want %d", len(got), err, ProbeLimit)
	}
	if got[0] != "f000.go" || got[len(got)-1] != "f049.go" {
		t.Fatalf("prefix probe bounds = %q .. %q, want f000.go .. f049.go", got[0], got[len(got)-1])
	}
}

// TestLocalPrefixProbeSkipsEscapingEntry: a symlink entry in the matching parent is
// skipped while its contained siblings still come back.
func TestLocalPrefixProbeSkipsEscapingEntry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "save_secret.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "save_ok.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "save_secret.go"), filepath.Join(root, "save_link.go")); err != nil {
		t.Fatal(err)
	}
	got, err := LocalPrefixProbe(root)(context.Background(), "save_")
	if err != nil || len(got) != 1 || got[0] != "save_ok.go" {
		t.Fatalf("prefix probe with escaping symlink = %v, %v; want only save_ok.go", got, err)
	}
}
