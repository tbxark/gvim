package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
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

// TestRemoteLargeFileOpensWindowedEditable checks a remote file at or above the
// large threshold opens in large-file mode through a windowed source: the whole-file
// read is never used (the fake would fail it), the status says so, the document is
// editable, and the app's window loader reports it as async.
func TestRemoteLargeFileOpensWindowedEditable(t *testing.T) {
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
	if doc == nil || !doc.AsyncLarge() || doc.ReadOnly() {
		t.Fatalf("active document = async %v readOnly %v", doc != nil && doc.AsyncLarge(), doc != nil && doc.ReadOnly())
	}
	if status := doc.OpenStatus(); !strings.Contains(status, "remote large file (windowed)") || strings.Contains(status, "read-only") {
		t.Fatalf("open status = %q, want the editable windowed reason", status)
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

// completeEditorRemoteIndex drives the editor's index jobs for a windowed remote
// large document, loading the window a step misses the way the root's window loop
// does. It returns the document.
func completeEditorRemoteIndex(t *testing.T, ed *editor.Editor, doc *document.Document) {
	t.Helper()
	ctx := context.Background()
	for range 4000 {
		job, ok := ed.PlanIndex()
		if !ok {
			break
		}
		ed.ApplyIndex(job.Run(ctx))
		if w, pending := doc.TakePendingLargeWindow(); pending {
			if err := doc.LoadLargeWindow(ctx, w); err != nil {
				t.Fatalf("LoadLargeWindow: %v", err)
			}
		}
	}
	if !doc.Index().Complete {
		t.Fatalf("remote large index not complete: %+v", doc.Index())
	}
}

// TestRemoteLargeEditAndSaveThroughEditor checks the whole user path: a remote large
// file is edited as an overlay, the save runs in a command (here synchronously), the
// merged bytes go through the fake authority's atomic write only, and the editor
// adopts the fresh windowed source in ApplySavePlan instead of reading a local file.
func TestRemoteLargeEditAndSaveThroughEditor(t *testing.T) {
	fs := newRemoteFS()
	path := filepath.Join(t.TempDir(), "remote-edit-large.txt")
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("line-content-")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteString("\n")
	}
	original := sb.String()
	fs.put(path, original)
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
	completeEditorRemoteIndex(t, ed, doc)
	// The first line's window is loaded so the edit can be made, as a painted
	// viewport guarantees.
	ctx := context.Background()
	for range 8 {
		if doc.Buffer.Line(0) != "" {
			break
		}
		w, pending := doc.TakePendingLargeWindow()
		if !pending {
			t.Fatal("no pending window for the first line")
		}
		if err := doc.LoadLargeWindow(ctx, w); err != nil {
			t.Fatalf("LoadLargeWindow: %v", err)
		}
	}
	doc.Buffer.Insert(buffer.Pos{Line: 0, Col: 0}, "EDIT ")
	if !doc.Buffer.Modified() {
		t.Fatal("the overlay edit did not mark the remote large document modified")
	}
	plan, err := ed.PlanSave()
	if err != nil {
		t.Fatalf("PlanSave: %v", err)
	}
	results := plan.Run()
	if err := ed.ApplySavePlan(plan, results); err != nil {
		t.Fatalf("ApplySavePlan: %v", err)
	}
	if doc.Buffer.Modified() {
		t.Fatal("the document is still modified after the save")
	}
	fs.mu.Lock()
	saved := append([]byte(nil), fs.files[path]...)
	fs.mu.Unlock()
	want := append([]byte("EDIT "), []byte(original)...)
	if !bytes.Equal(saved, want) {
		t.Fatalf("saved remote content differs: got %d bytes, want %d", len(saved), len(want))
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("the local path %q exists; the save used the local filesystem", path)
	}
	// The rebased buffer reads the file it just wrote through a fresh windowed
	// source, not a local file.
	if got := doc.Buffer.Line(0); !strings.HasPrefix(got, "EDIT line-content-") {
		t.Fatalf("line 0 after rebase = %q, want the saved edit", got)
	}
	if !doc.AsyncLarge() {
		t.Fatal("the saved document stopped being a windowed large file")
	}
}
