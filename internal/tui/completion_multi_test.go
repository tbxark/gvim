package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// openBufferWordPopup opens a local buffer-word completion list on tab and returns the
// completion state and the index of label.
func openBufferWordPopup(t *testing.T, lt *lspTest, label string) (*lspCompletion, int) {
	t.Helper()
	if cmd := lt.m.completionAsked(); cmd != nil {
		feedCompletion(t, lt, cmd)
	}
	comp := lt.m.lsp.comp
	if comp == nil {
		t.Fatalf("no completion popup opened")
	}
	for i, item := range comp.items {
		if item.Label == label {
			return comp, i
		}
	}
	t.Fatalf("item %q not in %v", label, completionLabels(lt))
	return nil, 0
}

// TestCompletionMultiCursorAccept: with extra carets the accepted completion goes to the
// editor's atomic per-caret path instead of the single-caret edit path.
func TestCompletionMultiCursorAccept(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
	comp, index := openBufferWordPopup(t, lt, "foobar")
	lt.m.acceptCompletion(comp, index)
	if len(lt.ed.multiApplies) != 1 {
		t.Fatalf("multi-caret accepts = %d, want 1", len(lt.ed.multiApplies))
	}
	apply := lt.ed.multiApplies[0]
	if apply.path != "/p/a.go" || apply.text != "foobar" {
		t.Fatalf("multi accept = %+v", apply)
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("accept left the completion popup open")
	}
	if lt.m.notice != "" {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorSnippetAndImport: a snippet/auto-import item now uses the
// editor's per-caret atomic path with the snippet's rune stop and the additional edits
// applied once; the popup closes and no refusal notice is shown.
func TestCompletionMultiCursorSnippetAndImport(t *testing.T) {
	t.Run("snippet", func(t *testing.T) {
		tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
		lt := newLSPTest(t, nil, tab)
		lt.open()
		lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 7}, true
		tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
		comp, _ := openBufferWordPopup(t, lt, "foobar")
		item := CompletionItem{Label: "call", Insert: "call()", Snippet: true, CursorOffset: 2}
		if !lt.m.multiCursorAccept(comp, item) {
			t.Fatal("multiCursorAccept did not handle the extra carets")
		}
		if len(lt.ed.multiItemApplies) != 1 {
			t.Fatalf("snippet accepts = %+v, want one", lt.ed.multiItemApplies)
		}
		got := lt.ed.multiItemApplies[0]
		if got.path != "/p/a.go" || got.text != "call()" || got.cursorOffset != 2 {
			t.Fatalf("snippet accept = %+v", got)
		}
		if len(got.additional) != 0 {
			t.Fatalf("snippet additional = %+v, want none", got.additional)
		}
		if got.expect == nil || got.expect.Ver != 7 {
			t.Fatalf("snippet version guard = %+v", got.expect)
		}
		if lt.m.lsp.comp != nil || lt.m.notice != "" {
			t.Fatalf("popup/notice after snippet = %v %q", lt.m.lsp.comp != nil, lt.m.notice)
		}
	})

	t.Run("auto-import", func(t *testing.T) {
		tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
		lt := newLSPTest(t, nil, tab)
		lt.open()
		tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
		comp, _ := openBufferWordPopup(t, lt, "foobar")
		item := CompletionItem{
			Label: "fmt", Insert: "fmt",
			Additional: []CompletionEdit{{Range: LSPRange{Start: buffer.Pos{Line: 0, Col: 0}, End: buffer.Pos{Line: 0, Col: 0}}, Text: "import fmt\n"}},
		}
		if !lt.m.multiCursorAccept(comp, item) {
			t.Fatal("multiCursorAccept did not handle the extra carets")
		}
		if len(lt.ed.multiItemApplies) != 1 {
			t.Fatalf("import accepts = %+v, want one", lt.ed.multiItemApplies)
		}
		got := lt.ed.multiItemApplies[0]
		if got.text != "fmt" || got.cursorOffset != -1 {
			t.Fatalf("import accept = %+v, want the insert and no snippet stop", got)
		}
		if len(got.additional) != 1 || got.additional[0].Text != "import fmt\n" {
			t.Fatalf("import additional = %+v", got.additional)
		}
		if lt.m.lsp.comp != nil || lt.m.notice != "" {
			t.Fatalf("popup/notice after import = %v %q", lt.m.lsp.comp != nil, lt.m.notice)
		}
	})
}

// multiResolveTest returns a multi-caret completion test at the start of a resolve
// with the request version set. The resolved-import item covers the lazy additional
// edits the reply supplies.
func multiResolveTest(t *testing.T) (*lspTest, *lspTab, *lspCompletion) {
	t.Helper()
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "foobar fo"}, buffer.Pos{Line: 1, Col: 9})
	lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 7}, true
	tab := lt.ed.tabs[0]
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	port.resolvedItem = CompletionItem{
		Label: "fmt", Insert: "fmt",
		Additional: []CompletionEdit{{
			Range: LSPRange{Start: buffer.Pos{Line: 0}, End: buffer.Pos{Line: 0}},
			Text:  "import \"fmt\"\n",
		}},
	}
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", ResolveToken: 7}}, buffer.Pos{Line: 1, Col: 9})
	return lt, tab, c
}

// TestCompletionMultiCursorResolveApplies: a lazily resolved item with extra carets is
// resolved in a command and, when the document and carets are unchanged, applied at
// every caret with the resolved additional edits in the editor's one atomic path.
func TestCompletionMultiCursorResolveApplies(t *testing.T) {
	lt, _, c := multiResolveTest(t)
	cmd := lt.m.acceptCompletion(c, 0)
	if cmd == nil {
		t.Fatal("accepting a multi-cursor resolve item did not start a resolve command")
	}
	if len(lt.ed.multiItemApplies) != 0 {
		t.Fatal("the resolve item was applied before its reply arrived")
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("the resolve accept left the completion popup open")
	}
	feedCommand(lt, cmd)
	if len(lt.port.resolveCalls) != 1 || lt.port.resolveCalls[0] != 7 {
		t.Fatalf("resolve calls = %v, want token 7", lt.port.resolveCalls)
	}
	if len(lt.ed.multiItemApplies) != 1 {
		t.Fatalf("multi-caret applies = %+v, want one", lt.ed.multiItemApplies)
	}
	got := lt.ed.multiItemApplies[0]
	if got.path != "/p/a.go" || got.text != "fmt" || got.cursorOffset != -1 {
		t.Fatalf("resolved accept = %+v", got)
	}
	if len(got.additional) != 1 || got.additional[0].Text != "import \"fmt\"\n" {
		t.Fatalf("resolved additional edits = %+v", got.additional)
	}
	if got.expect == nil || got.expect.Ver != 7 {
		t.Fatalf("resolved version guard = %+v", got.expect)
	}
	if lt.m.notice != "" {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorResolveStaleVersion: an edit while the resolve is in flight
// changes the document version, so the reply writes nothing and says why.
func TestCompletionMultiCursorResolveStaleVersion(t *testing.T) {
	lt, _, c := multiResolveTest(t)
	cmd := lt.m.acceptCompletion(c, 0)
	if cmd == nil {
		t.Fatal("no resolve command")
	}
	lt.ed.version = editor.Version{Ver: 8}
	feedCommand(lt, cmd)
	if len(lt.ed.multiItemApplies) != 0 {
		t.Fatalf("stale resolve applied: %+v", lt.ed.multiItemApplies)
	}
	if !strings.Contains(lt.m.notice, "stale") {
		t.Fatalf("notice = %q, want the stale refusal", lt.m.notice)
	}
}

// TestCompletionMultiCursorResolveCaretChanges: moving or collapsing a caret while the
// resolve runs refuses the reply, because the per-caret positions are no longer the ones
// the accept snapshotted.
func TestCompletionMultiCursorResolveCaretChanges(t *testing.T) {
	t.Run("moved", func(t *testing.T) {
		lt, tab, c := multiResolveTest(t)
		cmd := lt.m.acceptCompletion(c, 0)
		tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 3}}}
		feedCommand(lt, cmd)
		if len(lt.ed.multiItemApplies) != 0 {
			t.Fatalf("a moved caret still applied the resolve: %+v", lt.ed.multiItemApplies)
		}
		if !strings.Contains(lt.m.notice, "cursors changed") {
			t.Fatalf("notice = %q", lt.m.notice)
		}
	})
	t.Run("collapsed", func(t *testing.T) {
		lt, tab, c := multiResolveTest(t)
		cmd := lt.m.acceptCompletion(c, 0)
		tab.carets = nil
		feedCommand(lt, cmd)
		if len(lt.ed.multiItemApplies) != 0 {
			t.Fatalf("a collapsed caret set still applied the resolve: %+v", lt.ed.multiItemApplies)
		}
		if !strings.Contains(lt.m.notice, "cursors changed") {
			t.Fatalf("notice = %q", lt.m.notice)
		}
	})
}

// TestCompletionMultiCursorResolveWithoutVersionRefused: without a version port the
// accept cannot validate the async reply, so it refuses before sending anything.
func TestCompletionMultiCursorResolveWithoutVersionRefused(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "foobar fo"}, buffer.Pos{Line: 1, Col: 9})
	lt.ed.versionOK = false
	tab := lt.ed.tabs[0]
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", ResolveToken: 7}}, buffer.Pos{Line: 1, Col: 9})
	if cmd := lt.m.acceptCompletion(c, 0); cmd != nil {
		t.Fatal("a resolve without a version guard started a command")
	}
	if len(port.resolveCalls) != 0 || len(lt.ed.multiItemApplies) != 0 {
		t.Fatalf("refused resolve calls=%v applies=%+v", port.resolveCalls, lt.ed.multiItemApplies)
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("the refused accept left the popup open")
	}
	if !strings.Contains(lt.m.notice, "cannot verify the document version") {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorResolveInactiveDocument: switching to another document while
// the resolve runs refuses the reply instead of applying it to the new active tab.
func TestCompletionMultiCursorResolveInactiveDocument(t *testing.T) {
	lt, _, c := multiResolveTest(t)
	cmd := lt.m.acceptCompletion(c, 0)
	lt.ed.tabs = append(lt.ed.tabs, &lspTab{path: "/p/b.go", lines: []string{"package b"}})
	lt.ed.active = 1
	feedCommand(lt, cmd)
	if len(lt.ed.multiItemApplies) != 0 {
		t.Fatalf("a resolve applied after a document switch: %+v", lt.ed.multiItemApplies)
	}
	if !strings.Contains(lt.m.notice, "no longer active") {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorResolveFailure: a failed resolve reports the server error and
// writes nothing.
func TestCompletionMultiCursorResolveFailure(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "foobar fo"}, buffer.Pos{Line: 1, Col: 9})
	lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 7}, true
	lt.ed.tabs[0].carets = []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", ResolveToken: 7}}, buffer.Pos{Line: 1, Col: 9})
	cmd := lt.m.acceptCompletion(c, 0)
	port.resolveErr = errors.New("the server went away")
	feedCommand(lt, cmd)
	if len(lt.ed.multiItemApplies) != 0 {
		t.Fatalf("applied after a failed resolve: %+v", lt.ed.multiItemApplies)
	}
	if !strings.Contains(lt.m.notice, "server went away") {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorResolveSnippet: the resolved item's snippet stop is passed to
// the atomic per-caret editor path, so every caret lands inside its own insertion.
func TestCompletionMultiCursorResolveSnippet(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "foobar fo"}, buffer.Pos{Line: 1, Col: 9})
	lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 7}, true
	lt.ed.tabs[0].carets = []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	port.resolvedItem = CompletionItem{
		Label: "call", Insert: "call()", Snippet: true, CursorOffset: 2,
		Additional: []CompletionEdit{{
			Range: LSPRange{Start: buffer.Pos{Line: 0}, End: buffer.Pos{Line: 0}},
			Text:  "import \"pkg\"\n",
		}},
	}
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "call", ResolveToken: 9}}, buffer.Pos{Line: 1, Col: 9})
	feedCommand(lt, lt.m.acceptCompletion(c, 0))
	if len(lt.ed.multiItemApplies) != 1 {
		t.Fatalf("snippet resolve applies = %+v", lt.ed.multiItemApplies)
	}
	got := lt.ed.multiItemApplies[0]
	if got.text != "call()" || got.cursorOffset != 2 {
		t.Fatalf("snippet resolve = %+v, want the resolved text and stop", got)
	}
	if len(got.additional) != 1 || got.additional[0].Text != "import \"pkg\"\n" {
		t.Fatalf("snippet resolve additional = %+v", got.additional)
	}
}

// TestCompletionMultiCursorResolveOverlap: the resolved additional edit overlapping a
// caret replacement is refused by the editor path and reported without applying.
func TestCompletionMultiCursorResolveOverlap(t *testing.T) {
	lt, _, c := multiResolveTest(t)
	cmd := lt.m.acceptCompletion(c, 0)
	lt.ed.multiItemErr = editor.ErrOverlappingEdits
	feedCommand(lt, cmd)
	if len(lt.ed.multiItemApplies) != 1 {
		t.Fatalf("overlap apply = %+v", lt.ed.multiItemApplies)
	}
	if !strings.Contains(lt.m.notice, "overlap") {
		t.Fatalf("notice = %q", lt.m.notice)
	}
}

// TestCompletionMultiCursorSnippetStaleGuard: the snippet/import multi-caret path passes
// the request version and reports a stale refusal without applying anything.
func TestCompletionMultiCursorSnippetStaleGuard(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 9}, true
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
	comp, _ := openBufferWordPopup(t, lt, "foobar")
	lt.ed.multiItemErr = editor.ErrStale
	lt.m.multiCursorAccept(comp, CompletionItem{Label: "call", Insert: "call()", Snippet: true})
	if len(lt.ed.multiItemApplies) != 1 {
		t.Fatalf("snippet accepts = %+v", lt.ed.multiItemApplies)
	}
	if got := lt.ed.multiItemApplies[0].expect; got == nil || got.Ver != 9 {
		t.Fatalf("version guard = %+v", got)
	}
	if !strings.Contains(lt.m.notice, "stale") {
		t.Fatalf("notice = %q, want the stale refusal", lt.m.notice)
	}
}

// TestCompletionMultiCursorStaleGuard: the version captured at the request is passed to
// the multi-caret editor edit, and a stale refusal is reported without applying.
func TestCompletionMultiCursorStaleGuard(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
	lt := newLSPTest(t, nil, tab)
	lt.ed.version, lt.ed.versionOK = editor.Version{Ver: 42}, true
	lt.open()
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
	comp, index := openBufferWordPopup(t, lt, "foobar")
	lt.ed.multiErr = editor.ErrStale
	lt.m.acceptCompletion(comp, index)
	if len(lt.ed.multiApplies) != 1 {
		t.Fatalf("multi-caret accepts = %d, want 1", len(lt.ed.multiApplies))
	}
	if got := lt.ed.multiApplies[0].expect; got == nil || got.IsZero() {
		t.Fatalf("the multi-caret accept carried no expected version: %+v", got)
	}
	if !strings.Contains(lt.m.notice, "stale") {
		t.Fatalf("notice = %q, want the stale refusal", lt.m.notice)
	}
}

// TestCompletionSingleCursorPathUnchanged: without extra carets the accept still uses the
// single-caret LSP edit path, not the multi-caret one.
func TestCompletionSingleCursorPathUnchanged(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	comp, index := openBufferWordPopup(t, lt, "foobar")
	lt.m.acceptCompletion(comp, index)
	if len(lt.ed.multiApplies) != 0 {
		t.Fatalf("the single-caret accept used the multi-caret path: %+v", lt.ed.multiApplies)
	}
	if len(lt.ed.applied["/p/a.go"]) == 0 {
		t.Fatal("the single-caret accept applied no edit")
	}
}

// TestCompletionMultiCursorTypingAndBackspaceUseTheSameActions: typing and Backspace with
// a popup and extra carets route through insert_char/delete_backward (the editor actions
// that apply at every caret), and the filter follows the primary caret.
func TestCompletionMultiCursorTypingAndBackspaceUseTheSameActions(t *testing.T) {
	tab := &lspTab{path: "/p/a.go", lines: []string{"foobar fo"}, cursor: buffer.Pos{Line: 0, Col: 9}}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	tab.carets = []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 2}}}
	comp, _ := openBufferWordPopup(t, lt, "foobar")
	lt.m.completionTyped(comp, "o")
	if !completionActionRan(lt.ed.actions, "insert_char") {
		t.Fatalf("typing actions = %v, want insert_char", lt.ed.actions)
	}
	if comp := lt.m.lsp.comp; comp == nil {
		t.Fatal("typing closed the popup")
	}
	lt.m.completionBackspace(lt.m.lsp.comp)
	if !completionActionRan(lt.ed.actions, "delete_backward") {
		t.Fatalf("backspace actions = %v, want delete_backward", lt.ed.actions)
	}
}

// completionActionRan reports whether the recorded editor actions hold action. The
// explorer test package has a same-shaped helper for its own prefixed actions, so this
// one stays local to the completion tests.
func completionActionRan(actions []string, action string) bool {
	for _, got := range actions {
		if got == action {
			return true
		}
	}
	return false
}
