package uiadapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
)

func TestMoveFileTouchesOnlyTheDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	dst := filepath.Join(dir, "sub", "new.txt")
	taken := filepath.Join(dir, "taken.txt")
	for _, p := range []string{src, taken} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := &Editor{ed: editor.New(editor.Options{})}

	if moved, err := a.MoveFile(src, taken, true); moved || err != nil {
		t.Fatalf("MoveFile onto an existing target with ignoreIfExists = %v, %v; want false, nil", moved, err)
	}
	if moved, err := a.MoveFile(src, taken, false); moved || err == nil {
		t.Fatalf("MoveFile onto an existing target = %v, %v; want an error", moved, err)
	}
	if moved, err := a.MoveFile(src, dst, false); !moved || err != nil {
		t.Fatalf("MoveFile = %v, %v; want true, nil", moved, err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("the target is missing: %v", err)
	}
	a.SetRemote(true)
	if moved, err := a.MoveFile(dst, src, false); moved || err == nil {
		t.Fatalf("a remote Editor moved a local file: %v, %v", moved, err)
	}
}
