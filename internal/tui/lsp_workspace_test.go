package tui

import (
	"errors"
	"github.com/tbxark/gvim/document"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
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
		{Kind: WorkspaceOpText, Path: "/p/a.go", Edits: []document.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "package b"}}},
		{Kind: WorkspaceOpText, Path: "/p/b.go", Edits: []document.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 3}, Text: "bar"}}},
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

// TestWorkspaceEditRefusesStaleVersion checks the protocol version check: a text
// edit computed against an older document than the server now has is skipped (and
// the rest of the edit still runs), as the ordered pipeline applies each step when
// its turn comes.
func TestWorkspaceEditRefusesStaleVersion(t *testing.T) {
	port := newFakeLSP("go")
	stale := int32(0)
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{{
		Kind: WorkspaceOpText, Path: "/p/a.go", Version: &stale,
		Edits: []document.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "old"}},
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
	loaded := &document.Document{Path: "/p/b.go"}
	lt.ed.loadDocs = map[string]*document.Document{"/p/b.go": loaded}
	// The hash of "" is the hash of the document an empty loaded document gives;
	// any other hash is a mismatch.
	cmd := lt.m.ApplyFileEdits([]FileEdit{{Path: "/p/b.go", ExpectHash: 1, Edits: []document.LSPEdit{{Text: "x"}}}}, nil)
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

// TestWorkspaceEditInterleavesResourceOpsAndTextEdits checks the documentChanges
// ordering: the steps run in the order the server sent them, so a file is created,
// then read and edited, then renamed, and the log proves the order.
func TestWorkspaceEditInterleavesResourceOpsAndTextEdits(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{
		{Kind: WorkspaceOpCreate, Path: "/p/new.go"},
		{Kind: WorkspaceOpText, Path: "/p/new.go", Edits: []document.LSPEdit{{From: buffer.Pos{}, Text: "package new"}}},
		{Kind: WorkspaceOpRename, Path: "/p/new.go", NewPath: "/p/moved.go"},
	}}}
	lt.drain(lt.m.lspPoll())
	want := []string{
		"create /p/new.go",
		"load /p/new.go",
		"open /p/new.go",
		"edit /p/new.go",
		"rename /p/new.go /p/moved.go",
	}
	if !slicesEqual(lt.ed.opLog, want) {
		t.Fatalf("operation order = %v, want %v", lt.ed.opLog, want)
	}
	if got := lt.ed.applied["/p/new.go"]; len(got) != 1 || got[0].Text != "package new" {
		t.Fatalf("created file edits = %+v, want the text edit", got)
	}
	if !strings.Contains(lt.m.notice, "1 files changed") && !strings.Contains(lt.m.notice, "1 file changed") {
		t.Fatalf("notice = %q, want a changed-file count", lt.m.notice)
	}
}

// TestWorkspaceEditStopsAfterARefusedOperation checks the failure model: a refused
// resource operation stops the remaining documentChanges, and the notice reports
// the partial state (what applied, how many later changes did not) instead of
// claiming the edit landed whole.
func TestWorkspaceEditStopsAfterARefusedOperation(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{
		{Kind: WorkspaceOpDelete, Path: "/p/gone.go"},
		{Kind: WorkspaceOpText, Path: "/p/a.go", Edits: []document.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "package b"}}},
		{Kind: WorkspaceOpCreate, Path: "/p/later.go"},
	}}}
	lt.drain(lt.m.lspPoll())
	if len(lt.ed.applied["/p/a.go"]) != 0 {
		t.Fatalf("text edit after a refused delete was applied: %+v", lt.ed.applied["/p/a.go"])
	}
	if len(lt.ed.fileOps) != 0 {
		t.Fatalf("file operations after the refusal = %v, want none", lt.ed.fileOps)
	}
	for _, want := range []string{"delete /p/gone.go", "2 later changes were not applied", "only partly applied"} {
		if !strings.Contains(lt.m.notice, want) {
			t.Errorf("notice = %q, want it to contain %q", lt.m.notice, want)
		}
	}
}

// TestWorkspaceEditPartialSuccessBeforeARefusal checks the other side of the
// partial state: the operations before the refused one stay applied, and the
// notice counts them while saying the edit is only partial.
func TestWorkspaceEditPartialSuccessBeforeARefusal(t *testing.T) {
	port := newFakeLSP("go")
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: []string{"package a"}})
	lt.open()
	port.workspace = []WorkspaceEdit{{Ops: []WorkspaceOp{
		{Kind: WorkspaceOpCreate, Path: "/p/first.go"},
		{Kind: WorkspaceOpText, Path: "/p/a.go", Edits: []document.LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{Col: 9}, Text: "package b"}}},
		{Kind: WorkspaceOpDelete, Path: "/p/gone.go"},
	}}}
	lt.drain(lt.m.lspPoll())
	if got := lt.ed.applied["/p/a.go"]; len(got) != 1 {
		t.Fatalf("text edit before the refusal = %+v, want it applied", got)
	}
	if want := []string{"create /p/first.go"}; !slicesEqual(lt.ed.fileOps, want) {
		t.Fatalf("file operations = %v, want %v", lt.ed.fileOps, want)
	}
	for _, want := range []string{"1 file changed", "1 file operation", "refused"} {
		if !strings.Contains(lt.m.notice, want) {
			t.Errorf("notice = %q, want it to contain %q", lt.m.notice, want)
		}
	}
}

// splitRenameEd is an editor port with the split rename: the disk move is a separate call
// from the editor's check and follow, so a test can tell which goroutine each one is for.
type splitRenameEd struct {
	*lspEd
	log      []string
	checkErr error
}

func (e *splitRenameEd) CheckRename(oldPath, newPath string) error {
	e.log = append(e.log, "check "+oldPath+" "+newPath)
	return e.checkErr
}

func (e *splitRenameEd) MoveFile(oldPath, newPath string, _ bool) (bool, error) {
	e.log = append(e.log, "move "+oldPath+" "+newPath)
	return true, nil
}

func (e *splitRenameEd) FollowRename(oldPath, newPath string) error {
	e.log = append(e.log, "follow "+oldPath+" "+newPath)
	return nil
}

// TestWorkspaceEditRenameKeepsTheEditorOnTheUIGoroutine checks the split of an LSP file
// rename: the editor is read in the call that builds the command, only the disk move runs
// inside the command, and the open tabs follow when the command's message comes back to
// Update. RenameFile, which does both, is not used for a port with the split.
func TestWorkspaceEditRenameKeepsTheEditorOnTheUIGoroutine(t *testing.T) {
	tests := []struct {
		name     string
		checkErr error
		wantLog  []string
		wantSkip string
	}{
		{name: "renames", wantLog: []string{"check /p/old.go /p/new.go", "move /p/old.go /p/new.go", "follow /p/old.go /p/new.go"}},
		{name: "refused by an open tab", checkErr: errors.New("new.go is open"), wantLog: []string{"check /p/old.go /p/new.go"}, wantSkip: "new.go is open"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt := newLSPTest(t, newFakeLSP("go"), &lspTab{path: "/p/a.go", lines: []string{"package a"}})
			ed := &splitRenameEd{lspEd: lt.ed, checkErr: tt.checkErr}
			lt.m.ed = ed
			var res FileEditResult
			cmd := lt.m.applyWorkspaceEdit(WorkspaceEdit{Ops: []WorkspaceOp{{Kind: WorkspaceOpRename, Path: "/p/old.go", NewPath: "/p/new.go"}}},
				func(r FileEditResult) tea.Cmd { res = r; return nil })
			if tt.wantSkip != "" {
				if cmd != nil || len(res.Skipped) == 0 || !strings.Contains(res.Skipped[0], tt.wantSkip) {
					t.Fatalf("refused rename: cmd=%v skipped=%v", cmd != nil, res.Skipped)
				}
			} else {
				if cmd == nil {
					t.Fatal("a rename returned no command for its disk move")
				}
				if want := []string{"check /p/old.go /p/new.go"}; !slicesEqual(ed.log, want) {
					t.Fatalf("before the command ran: log = %v, want %v", ed.log, want)
				}
				msg := cmd()
				if want := []string{"check /p/old.go /p/new.go", "move /p/old.go /p/new.go"}; !slicesEqual(ed.log, want) {
					t.Fatalf("the command did more than the disk move: log = %v", ed.log)
				}
				lt.m.Update(msg)
				if res.Operations != 1 {
					t.Fatalf("operations = %d, want 1 (skipped %v)", res.Operations, res.Skipped)
				}
			}
			if !slicesEqual(ed.log, tt.wantLog) {
				t.Fatalf("log = %v, want %v", ed.log, tt.wantLog)
			}
			if len(ed.fileOps) != 0 {
				t.Fatalf("RenameFile ran as well: %v", ed.fileOps)
			}
		})
	}
}

// TestWorkspaceEditWithoutFileOperationsStillReports checks that an editor port with no file
// operations stops the plan like a refused step: the report runs with the reason and the
// tab the edit started in is active again, instead of the plan being dropped half way.
func TestWorkspaceEditWithoutFileOperationsStillReports(t *testing.T) {
	lt := newLSPTest(t, newFakeLSP("go"),
		&lspTab{path: "/p/a.go", lines: []string{"package a"}}, &lspTab{path: "/p/b.go", lines: []string{"package b"}})
	lt.ed.active = 0
	lt.m.ed = struct{ EditorState }{lt.ed}
	var res FileEditResult
	reported := false
	cmd := lt.m.applyWorkspaceEdit(WorkspaceEdit{Ops: []WorkspaceOp{
		{Kind: WorkspaceOpRename, Path: "/p/old.go", NewPath: "/p/new.go"},
		{Kind: WorkspaceOpCreate, Path: "/p/later.go"},
	}}, func(r FileEditResult) tea.Cmd { res, reported = r, true; return nil })
	if cmd != nil {
		t.Fatal("the plan returned a command with no file operations to run")
	}
	if !reported {
		t.Fatal("the report did not run: the plan was dropped")
	}
	if len(res.Skipped) != 2 || !strings.Contains(res.Skipped[0], "file operations") || !strings.Contains(res.Skipped[1], "1 later changes") {
		t.Fatalf("skipped = %v", res.Skipped)
	}
}
