package uiadapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestEditorWorkspaceOpsAndRemoteIsolation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "old.txt")
	dst := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(src, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	a := &Editor{ed: ed}

	// Conflict without ignoreIfExists must refuse without overwriting dst.
	if err := a.RenameFile(src, dst, false); err == nil {
		t.Fatal("RenameFile onto existing target succeeded")
	}
	gotDst, err := os.ReadFile(dst)
	if err != nil || string(gotDst) != "keep" {
		t.Fatalf("dst = %q, %v; want keep", gotDst, err)
	}

	// Remote mode must refuse local CreateFile and RenameFile.
	a.SetRemote(true)
	if err := a.CreateFile(filepath.Join(dir, "remote-only.txt"), false); err == nil {
		t.Fatal("remote Editor.CreateFile touched local disk")
	}
	if _, err := os.Stat(filepath.Join(dir, "remote-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("local file unexpectedly created: %v", err)
	}
	if err := a.RenameFile(src, filepath.Join(dir, "moved.txt"), false); err == nil {
		t.Fatal("remote Editor.RenameFile renamed local disk")
	}

	// RemoteDialogs SaveAs returns an overlay cleanly without local Validate/Exists checks.
	ov := RemoteDialogs().SaveAs("/srv/remote/missing/a.txt", "tag", style.Overlay{})
	if ov == nil {
		t.Fatal("RemoteDialogs().SaveAs returned nil")
	}
}
