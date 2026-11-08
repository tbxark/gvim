package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/editorpane"
)

// runMsgs feeds messages and the commands they start back into the model, for a
// few rounds: what an action produces in the tests.
func (lt *lspTest) runMsgs(msgs []tea.Msg) {
	lt.t.Helper()
	for range 12 {
		if len(msgs) == 0 {
			return
		}
		var next []tea.Msg
		for _, msg := range msgs {
			next = append(next, lt.feed(msg)...)
		}
		msgs = next
	}
}

// drain runs cmd and feeds back every message its commands produce, until no
// command starts another.
func (lt *lspTest) drain(cmd tea.Cmd) {
	lt.t.Helper()
	msgs := deliver(cmd)
	for range 12 {
		if len(msgs) == 0 {
			return
		}
		var next []tea.Msg
		for _, msg := range msgs {
			next = append(next, lt.feed(msg)...)
		}
		msgs = next
	}
}

// TestWorkspaceEditAppliesTextAcrossFiles checks the server-initiated applyEdit
// flow: the poll drains the queued edit, the open document is edited now, and the
// file that is not open is read, opened as an unsaved tab, and edited.
func TestWorkspaceEditAppliesTextAcrossFiles(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Label: "organize imports", Ops: []WorkspaceOp{
		{Kind: WorkspaceOpText, Path: "/p/a.go", Edits: []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "package b"}}},
		{Kind: WorkspaceOpText, Path: "/p/b.go", Edits: []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 3}, Text: "bar"}}},
	}}}
	lt.drain(lt.m.lspPoll())
	if got := lt.ed.applied["/p/a.go"]; len(got) != 1 || got[0].Text != "package b" {
		t.Fatalf("open document edits = %+v, want the workspace edit", got)
	}
	if got := lt.ed.applied["/p/b.go"]; len(got) != 1 || got[0].Text != "bar" {
		t.Fatalf("closed file edits = %+v, want the loaded edit (opened %v, notice %q, overlays %d)", got, lt.ed.opened, lt.m.notice, len(lt.m.overlays))
	}
	if !strings.Contains(lt.m.notice, "organize imports") || !strings.Contains(lt.m.notice, "2 files changed") {
		t.Fatalf("notice = %q, want the label and the file count", lt.m.notice)
	}
}

// TestWorkspaceEditRefusesStaleVersion checks the protocol version check: an edit
// computed against an older document than the server now has is refused whole.
func TestWorkspaceEditRefusesStaleVersion(t *testing.T) {
	port := newFakeLSP("go")
	stale := int32(0)
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{{
		Kind: WorkspaceOpText, Path: "/p/a.go", Version: &stale,
		Edits: []editor.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "old"}},
	}}}}
	lt.drain(lt.m.lspPoll())
	if len(lt.ed.applied["/p/a.go"]) != 0 {
		t.Fatalf("a stale edit was applied: %+v", lt.ed.applied["/p/a.go"])
	}
	if !strings.Contains(lt.m.notice, "older version") {
		t.Fatalf("notice = %q, want the stale-edit refusal", lt.m.notice)
	}
}

// TestWorkspaceEditResourceOperations checks the file operations of a server edit:
// a create is made, a rename moves the file, and a server-requested delete is
// refused and reported without claiming success.
func TestWorkspaceEditResourceOperations(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{
		{Kind: WorkspaceOpCreate, Path: "/p/new.go"},
		{Kind: WorkspaceOpRename, Path: "/p/old.go", NewPath: "/p/moved.go"},
		{Kind: WorkspaceOpDelete, Path: "/p/gone.go"},
	}}}
	lt.drain(lt.m.lspPoll())
	if want := []string{"create /p/new.go", "rename /p/old.go /p/moved.go"}; !slicesEqual(lt.ed.fileOps, want) {
		t.Fatalf("file operations = %v, want %v", lt.ed.fileOps, want)
	}
	if !strings.Contains(lt.m.notice, "delete /p/gone.go") && !strings.Contains(lt.m.notice, "refused") {
		t.Fatalf("notice = %q, want the delete refusal", lt.m.notice)
	}
}

// TestApplyFileEditsSkipsAChangedClosedFile checks the shared helper's text-hash
// check: a file read back with different text than the one the edits were planned
// from is skipped, not overwritten.
func TestApplyFileEditsSkipsAChangedClosedFile(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	loaded := &editor.Document{Path: "/p/b.go"}
	lt.ed.loadDocs = map[string]*editor.Document{"/p/b.go": loaded}
	// The hash of "" is the hash of the document an empty loaded document gives;
	// any other hash is a mismatch.
	cmd := lt.m.ApplyFileEdits([]FileEdit{{Path: "/p/b.go", ExpectHash: 1, Edits: []editor.LSPEdit{{Text: "x"}}}}, nil)
	lt.settle(cmd, 3)
	if len(lt.ed.applied["/p/b.go"]) != 0 {
		t.Fatalf("a file whose text changed was edited: %+v", lt.ed.applied["/p/b.go"])
	}
}

// TestDiagnosticsCarryTheirVersionForStaleness checks the plumbed version: a
// diagnostic set published for an older document is marked stale for the inline
// renderer, and one published for the current document is not.
func TestDiagnosticsCarryTheirVersionForStaleness(t *testing.T) {
	port := newFakeLSP("go")
	tab := &lspTab{path: "/p/a.go", lines: []string{"package a"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	lt.feed(lspOpenedMsg{path: "/p/a.go", text: "package a", version: 5})

	spans := func() editorpane.DiagnosticSpan {
		doc := lt.m.ed.Active()
		f := lt.m.lspSpans(doc)
		if f == nil {
			t.Fatal("no diagnostic spans")
		}
		out := f(0)
		if len(out) != 1 {
			t.Fatalf("spans = %+v, want one", out)
		}
		return out[0]
	}
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Version: 4, Versioned: true,
		Items: []Diagnostic{{Start: buffer.Pos{}, End: buffer.Pos{Col: 7}, Severity: 1, Message: "boom"}}}}})
	if !spans().Stale {
		t.Fatal("diagnostics published for an older version are not stale")
	}
	lt.feed(lspDiagsMsg{events: []DiagnosticsEvent{{Path: "/p/a.go", Version: 5, Versioned: true,
		Items: []Diagnostic{{Start: buffer.Pos{}, End: buffer.Pos{Col: 7}, Severity: 1, Message: "boom"}}}}})
	if spans().Stale {
		t.Fatal("diagnostics published for the current version are stale")
	}
}

// TestLSPMuteStopsTheLanguage checks the language mute from the status popup: the
// port is told, the language's tracks are forgotten, and the pill shows the
// language as off while a server is configured.
func TestLSPMuteStopsTheLanguage(t *testing.T) {
	port := newFakeLSP("go")
	port.infoGo = true
	port.servers = []LSPServerStatus{{Language: "go", Name: "gopls", State: LSPServerRunning}}
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	lt.settle(lt.m.lspSetLanguageEnabled(false), 3)
	if !port.muted["go"] || !lt.m.lsp.muted["go"] {
		t.Fatalf("muted: port %v root %v", port.muted, lt.m.lsp.muted)
	}
	if len(lt.m.lsp.tracks) != 0 {
		t.Fatalf("tracks = %v after the mute, want none", lt.m.lsp.tracks)
	}
	if got := lt.m.lspPill(); got != "LSP (off)" {
		t.Fatalf("pill = %q, want LSP (off)", got)
	}
	if lt.m.lsp.enabled("go") {
		t.Fatal("a muted language is enabled")
	}
}

// slicesEqual compares two string slices.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = tea.KeyPressMsg{}
