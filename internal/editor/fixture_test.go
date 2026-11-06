package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// openFixture writes content to a new file in a temp dir and opens it in an
// editor built from opts. It returns the editor and the file path.
func openFixture(t *testing.T, opts Options, content string) (*Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(opts)
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e, path
}

// docWith returns an editor built from opts whose only tab holds text, with
// the caret at the start. Nothing is read from disk.
func docWith(opts Options, text string) *Editor {
	e := New(opts)
	e.docs = []*Document{newDocument("", text)}
	e.switchTo(0)
	return e
}
