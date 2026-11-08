package uiadapter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

func TestApplyLSPEditsThroughTheEditorAdapter(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a := &Editor{ed: ed}
	edit := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 6}, To: buffer.Pos{Line: 0, Col: 11}, Text: "gvim"}
	if err := a.ApplyLSPEdits(path, []editor.LSPEdit{edit}); err != nil {
		t.Fatalf("ApplyLSPEdits: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "hello gvim\n" {
		t.Fatalf("text = %q, want the edit applied", got)
	}
}
