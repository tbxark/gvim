package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// ResolveCompletion answers a token with the canned item, so fakeLSP satisfies the
// optional CompletionResolver that acceptance uses for auto-imports.
func (f *fakeLSP) ResolveCompletion(path string, token int) (CompletionItem, error) {
	f.resolveCalls = append(f.resolveCalls, token)
	if f.resolveErr != nil {
		return CompletionItem{}, f.resolveErr
	}
	return f.resolvedItem, nil
}

func completionTest(t *testing.T, port *fakeLSP, lines []string, cursor buffer.Pos) *lspTest {
	t.Helper()
	return newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: lines, cursor: cursor})
}

func openLSPCompletion(t *testing.T, lt *lspTest, items []CompletionItem, at buffer.Pos) *lspCompletion {
	t.Helper()
	lt.m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: "/p/a.go", at: at, items: items})
	c := lt.m.lsp.comp
	if c == nil {
		t.Fatal("the completion popup did not open")
	}
	return c
}

func feedCommand(lt *lspTest, cmd tea.Cmd) {
	lt.t.Helper()
	if cmd == nil {
		return
	}
	for _, msg := range deliver(cmd) {
		_, _ = lt.m.Update(msg)
	}
}

// TestCompletionAdditionalEditsApplyAsOneGuardedEdit checks an auto-import item is
// inserted together with its additional edit in one call guarded by the version the
// list was asked at, and that the caret moves into the inserted text on the line the
// import shift leaves it on.
func TestCompletionAdditionalEditsApplyAsOneGuardedEdit(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "", "func f() {"}, buffer.Pos{Line: 2})
	lt.ed.version, lt.ed.versionOK = editor.Version{}, true
	main := LSPRange{Start: buffer.Pos{Line: 2}, End: buffer.Pos{Line: 2}}
	importEdit := CompletionEdit{
		Range: LSPRange{Start: buffer.Pos{Line: 0}, End: buffer.Pos{Line: 0}},
		Text:  "import \"fmt\"\n",
	}
	c := openLSPCompletion(t, lt, []CompletionItem{{
		Label: "fmt", Insert: "fmt", Range: &main, Additional: []CompletionEdit{importEdit},
	}}, main.Start)
	if cmd := lt.m.acceptCompletion(c, 0); cmd != nil {
		feedCommand(lt, cmd)
	}
	applied := lt.ed.applied["/p/a.go"]
	if len(applied) != 2 {
		t.Fatalf("applied = %+v, want the insert and the import in one edit", applied)
	}
	if lt.ed.expectCalls != 1 || lt.ed.expect == nil || *lt.ed.expect != lt.ed.version {
		t.Fatalf("expected-version guard = %+v after %d calls, want the captured version", lt.ed.expect, lt.ed.expectCalls)
	}
	if got := lt.ed.tabs[0].cursor; got != (buffer.Pos{Line: 3, Col: 3}) {
		t.Fatalf("caret = %+v, want the inserted text after the import line moved it down", got)
	}
}

// TestCompletionIsRefusedWhenTheDocumentChanged checks a stale item writes nothing:
// the editor is asked with the expected version and the error becomes a notice.
func TestCompletionIsRefusedWhenTheDocumentChanged(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a"}, buffer.Pos{Line: 0})
	lt.ed.version, lt.ed.versionOK = editor.Version{}, true
	lt.ed.applyErr = editor.ErrStale
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", Insert: "fmt"}}, buffer.Pos{})
	lt.m.acceptCompletion(c, 0)
	if len(lt.ed.applied) != 0 {
		t.Fatalf("applied = %+v after a stale item, want nothing", lt.ed.applied)
	}
	if !strings.Contains(lt.m.notice, "stale") {
		t.Fatalf("notice = %q, want an explanation about the stale completion", lt.m.notice)
	}
}

// TestCompletionRefusesOverlappingEdits checks an item whose additional edit overlaps
// the main edit is refused before the editor is asked at all.
func TestCompletionRefusesOverlappingEdits(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "abc"}, buffer.Pos{Line: 1, Col: 3})
	lt.ed.version, lt.ed.versionOK = editor.Version{}, true
	main := LSPRange{Start: buffer.Pos{Line: 1}, End: buffer.Pos{Line: 1, Col: 3}}
	c := openLSPCompletion(t, lt, []CompletionItem{{
		Label: "abcd", Insert: "abcd", Range: &main,
		Additional: []CompletionEdit{{Range: LSPRange{
			Start: buffer.Pos{Line: 1, Col: 1}, End: buffer.Pos{Line: 1, Col: 2},
		}, Text: "X"}},
	}}, main.Start)
	lt.m.acceptCompletion(c, 0)
	if len(lt.ed.applied) != 0 || lt.ed.expectCalls != 0 {
		t.Fatalf("applied = %+v calls = %d, want a refusal before any edit", lt.ed.applied, lt.ed.expectCalls)
	}
	if !strings.Contains(lt.m.notice, "overlap") {
		t.Fatalf("notice = %q, want an overlap explanation", lt.m.notice)
	}
}

// TestCompletionSnippetPlacesTheCaretAtTheFinalStop checks the expanded snippet's
// cursor offset is used instead of the end of the inserted text.
func TestCompletionSnippetPlacesTheCaretAtTheFinalStop(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "fo"}, buffer.Pos{Line: 1, Col: 2})
	main := LSPRange{Start: buffer.Pos{Line: 1}, End: buffer.Pos{Line: 1, Col: 2}}
	c := openLSPCompletion(t, lt, []CompletionItem{{
		Label: "foo", Insert: "foo()", Range: &main, Snippet: true, CursorOffset: 4,
	}}, main.Start)
	lt.m.acceptCompletion(c, 0)
	if got := lt.ed.tabs[0].cursor; got != (buffer.Pos{Line: 1, Col: 4}) {
		t.Fatalf("caret = %+v, want the snippet's final stop", got)
	}
}

// TestCompletionResolveThenApplies checks an incomplete item is resolved in a command
// and the resolved auto-import is applied with the same version guard, without
// blocking Update.
func TestCompletionResolveThenApplies(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "", "func f() {"}, buffer.Pos{Line: 2})
	lt.ed.version, lt.ed.versionOK = editor.Version{}, true
	main := LSPRange{Start: buffer.Pos{Line: 2}, End: buffer.Pos{Line: 2}}
	port.resolvedItem = CompletionItem{
		Label: "fmt", Insert: "fmt", Range: &main,
		Additional: []CompletionEdit{{
			Range: LSPRange{Start: buffer.Pos{Line: 0}, End: buffer.Pos{Line: 0}},
			Text:  "import \"fmt\"\n",
		}},
	}
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", ResolveToken: 7}}, main.Start)
	cmd := lt.m.acceptCompletion(c, 0)
	if cmd == nil {
		t.Fatal("accepting an unresolved item did not start a resolve command")
	}
	feedCommand(lt, cmd)
	if len(port.resolveCalls) != 1 || port.resolveCalls[0] != 7 {
		t.Fatalf("resolve calls = %v, want token 7", port.resolveCalls)
	}
	if got := len(lt.ed.applied["/p/a.go"]); got != 2 {
		t.Fatalf("applied %d edits after resolve, want the insert and the import", got)
	}
	if lt.ed.expect == nil || *lt.ed.expect != lt.ed.version {
		t.Fatalf("expected version = %+v, want the captured one", lt.ed.expect)
	}
}

// TestCompletionResolveFailureChangesNothing checks a failed resolve is a notice and
// writes no text.
func TestCompletionResolveFailureChangesNothing(t *testing.T) {
	port := newFakeLSP("go")
	lt := completionTest(t, port, []string{"package a", "fo"}, buffer.Pos{Line: 1, Col: 2})
	port.resolveErr = errors.New("the server went away")
	c := openLSPCompletion(t, lt, []CompletionItem{{Label: "fmt", ResolveToken: 1}}, buffer.Pos{Line: 1, Col: 2})
	feedCommand(lt, lt.m.acceptCompletion(c, 0))
	if len(lt.ed.applied) != 0 {
		t.Fatalf("applied = %+v after a failed resolve, want nothing", lt.ed.applied)
	}
	if !strings.Contains(lt.m.notice, "server went away") {
		t.Fatalf("notice = %q, want the resolve error", lt.m.notice)
	}
}
