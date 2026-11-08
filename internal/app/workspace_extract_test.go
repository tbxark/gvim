package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/session"
)

// extractionRoot makes a file in a fresh folder: the folder a tab extracted from it is
// rooted at.
func extractionRoot(t *testing.T, name, text string) (root, path string) {
	t.Helper()
	root = t.TempDir()
	dir := filepath.Join(root, "sub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, path
}

// TestWorkspaceExtractCarriesUnsavedTab is the #19b first slice: extracting the active tab
// creates a workspace at the file's folder, writes its session with the tab's cursor and a
// stash of its unsaved text, closes the source tab, and switches to it. The tab's text is
// never dropped and no disk write happens in Update: the command does all of it.
func TestWorkspaceExtractCarriesUnsavedTab(t *testing.T) {
	m, foregroundID := newWorkspaceManagerTest(t)
	_, path := extractionRoot(t, "a.txt", "one\ntwo\n")
	m.mu.Lock()
	sess := m.sessions[foregroundID]
	m.mu.Unlock()
	openInModel(t, sess.rt.model, path)
	doc := sess.rt.ed.Active()
	if doc == nil || doc.Path != path {
		t.Fatalf("active document = %+v", doc)
	}
	doc.InsertText("NEW-")
	doc.Cursor = buffer.Pos{Line: 1, Col: 2}
	doc.Top = 1

	cmd, err := m.ExtractTab()
	if err != nil {
		t.Fatalf("ExtractTab = %v", err)
	}
	if cmd == nil {
		t.Fatal("ExtractTab returned no command")
	}
	// The disk work is deferred to the command (Update does no I/O): nothing is created
	// and the tab is still open yet.
	if got := len(m.Entries()); got != 1 {
		t.Fatalf("extract created a workspace inside Update: %+v", m.Entries())
	}
	if !strings.Contains(doc.Buffer.Text(), "NEW-") {
		t.Fatal("the unsaved text disappeared before the command ran")
	}

	cmd()
	entries := m.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries after extraction = %+v", entries)
	}
	var destID string
	for _, e := range entries {
		if e.ID != foregroundID {
			destID = e.ID
			if !e.Active || !e.Materialized {
				t.Fatalf("new workspace = %+v, want active materialized", e)
			}
			wantRoot, err := daemon.CanonicalRoot(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if e.Root != wantRoot {
				t.Fatalf("new workspace root = %s, want %s", e.Root, wantRoot)
			}
		}
	}
	if destID == "" {
		t.Fatal("no extracted workspace")
	}
	// The source tab was force-closed without asking (the text is in the new session).
	for _, tab := range sess.rt.ed.Tabs() {
		if tab.Path == path {
			t.Fatal("the source tab is still open after a successful extraction")
		}
	}
	// The new workspace's session carries the file at the saved cursor with the stash.
	dir, err := m.reg.SessionDir(destID)
	if err != nil {
		t.Fatal(err)
	}
	layout, ok, err := session.Load(dir)
	if err != nil || !ok {
		t.Fatalf("load the extracted session: ok=%v err=%v", ok, err)
	}
	if len(layout.Files) != 1 {
		t.Fatalf("extracted layout = %+v", layout)
	}
	fs := layout.Files[0]
	if fs.Path != path || fs.Line != 2 || fs.Col != 3 || fs.Top != 1 {
		t.Fatalf("extracted file state = %+v, want path, line 2, col 3, top 1", fs)
	}
	if fs.StashFile == "" {
		t.Fatal("the unsaved text was not stashed into the new workspace")
	}
	stash, err := os.ReadFile(filepath.Join(dir, fs.StashFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(stash) != "NEW-one\ntwo\n" {
		t.Fatalf("stash = %q, want the unsaved text", stash)
	}
	// The file on disk is untouched and the report names the new workspace.
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("the file was written by the extraction: %q, %v", raw, err)
	}
	if !strings.Contains(m.lastNotice, "Extracted a.txt into workspace") {
		t.Fatalf("report = %q", m.lastNotice)
	}
	if err := m.waitWrites(10e9); err != nil {
		t.Fatalf("capture write: %v", err)
	}
}

// TestWorkspaceExtractRefusals checks the cases this slice refuses instead of dropping the
// tab: no file path, the same root (co-tenant workspaces are #19b), and an existing
// workspace for the file's folder. The tab stays open in every case.
func TestWorkspaceExtractRefusals(t *testing.T) {
	t.Run("no file path", func(t *testing.T) {
		m, _ := newWorkspaceManagerTest(t)
		if _, err := m.ExtractTab(); err == nil || !strings.Contains(err.Error(), "no file path") {
			t.Fatalf("ExtractTab on the welcome screen = %v", err)
		}
	})
	t.Run("same root", func(t *testing.T) {
		m, foregroundID := newWorkspaceManagerTest(t)
		m.mu.Lock()
		root := m.sessions[foregroundID].ws.File.Root
		m.mu.Unlock()
		path := filepath.Join(root, "a.txt")
		if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		openInModel(t, m.ForegroundModel(), path)
		cmd, err := m.ExtractTab()
		if err != nil {
			t.Fatalf("ExtractTab = %v", err)
		}
		cmd()
		if !strings.Contains(m.lastNotice, "already the workspace root") {
			t.Fatalf("same-root report = %q", m.lastNotice)
		}
		if got := len(m.Entries()); got != 1 {
			t.Fatalf("a refused extraction created a workspace: %+v", m.Entries())
		}
		if !tabOpen(m, path) {
			t.Fatal("a refused extraction closed the tab")
		}
	})
	t.Run("existing workspace", func(t *testing.T) {
		m, _ := newWorkspaceManagerTest(t)
		root, path := extractionRoot(t, "a.txt", "x\n")
		_ = root
		if _, created, err := m.reg.Create(filepath.Dir(path), "already"); err != nil || !created {
			t.Fatalf("seed workspace = %v, %v", created, err)
		}
		openInModel(t, m.ForegroundModel(), path)
		cmd, err := m.ExtractTab()
		if err != nil {
			t.Fatalf("ExtractTab = %v", err)
		}
		cmd()
		if !strings.Contains(m.lastNotice, "already exists") {
			t.Fatalf("report = %q", m.lastNotice)
		}
		if !tabOpen(m, path) {
			t.Fatal("a refused extraction closed the tab")
		}
	})
}

// tabOpen reports whether the active workspace still has path open in a tab.
func tabOpen(m *workspaceManager, path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess := m.sessions[m.active]
	if sess == nil {
		return false
	}
	for _, tab := range sess.rt.ed.Tabs() {
		if tab.Path == path {
			return true
		}
	}
	return false
}
