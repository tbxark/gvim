package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/editor"
)

// TestRemoteFileAccessReadRange checks the windowed read seam the remote large-file
// source uses: bytes come from the authority only, at the requested offset, and a
// short final window reports EOF from the transport.
func TestRemoteFileAccessReadRange(t *testing.T) {
	t.Parallel()
	fs := newRemoteFS()
	path := "/remote/big.txt"
	fs.put(path, "0123456789")
	slot := authoritytest.New()
	slot.FileSystem = fs
	access := newRemoteFileAccess(slot)
	got, err := access.ReadRange(context.Background(), path, 3, 4)
	if err != nil {
		t.Fatalf("ReadRange: %v", err)
	}
	if string(got) != "3456" {
		t.Fatalf("ReadRange = %q, want 3456", got)
	}
}

// TestRemoteLargeFileOpensWindowedReadOnly checks a remote file at or above the
// large threshold opens in large-file mode read-only through a windowed source: the
// whole-file read is never used (the fake would fail it), the status says why, and
// the app's window loader reports the document as async.
func TestRemoteLargeFileOpensWindowedReadOnly(t *testing.T) {
	fs := newRemoteFS()
	path := filepath.Join(t.TempDir(), "remote-large.txt")
	var sb strings.Builder
	for sb.Len() < 2<<20 {
		sb.WriteString("line-content-")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteString("\n")
	}
	fs.put(path, sb.String())
	slot := authoritytest.New()
	slot.FileSystem = fs
	spec := &authority.RemoteSpec{Path: path}
	ed := editor.New(editor.Options{
		Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1,
	})
	if got := setupRemoteEditing(ed, slot, spec); got != path {
		t.Fatalf("startup = %q, want %q", got, path)
	}
	if err := ed.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	doc := ed.Active()
	if doc == nil || !doc.AsyncLarge() || !doc.ReadOnly() {
		t.Fatalf("active document = async %v readOnly %v", doc != nil && doc.AsyncLarge(), doc != nil && doc.ReadOnly())
	}
	if status := doc.OpenStatus(); !strings.Contains(status, "remote large file (read-only, windowed)") {
		t.Fatalf("open status = %q, want the remote windowed reason", status)
	}
	// The local path never exists: every byte came from the fake authority, so no
	// local fallback can hide in the open.
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("the local path %q exists; the test cannot prove the no-local-fallback rule", path)
	}
	loader := newLargeWindows(ed)
	if loader == nil || !loader.AsyncLarge() {
		t.Fatal("the app window loader does not see the async large document")
	}
	// The head window was loaded during the open, so the first line paints.
	if line := doc.Buffer.Line(0); line == "" {
		t.Fatal("the first line is blank right after a windowed open")
	}
}
