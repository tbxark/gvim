package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// triggerLSP advertises completion trigger characters on top of a fake server.
type triggerLSP struct {
	*fakeLSP
	chars []string
}

func (t *triggerLSP) CompletionTriggerCharacters(string) []string { return t.chars }

// completionTestModel is a one-tab LSP test model with a configured active line.
func completionTestModel(t *testing.T, port *fakeLSP, lines []string, cursor buffer.Pos) *lspTest {
	t.Helper()
	lt := newLSPTest(t, port, &lspTab{path: "/p/a.go", lines: lines, cursor: cursor})
	lt.open()
	return lt
}

func TestCompletionTriggerCharacterOpensImmediately(t *testing.T) {
	timer := captureAutoTimer(t)
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	lt := completionTestModel(t, port, []string{"foo"}, buffer.Pos{Line: 0, Col: 3})
	lt.m.lsp.port = &triggerLSP{fakeLSP: port, chars: []string{"."}}
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.SuggestOnTriggerCharacters = true

	msgs := typeAutoChar(t, lt, '.')
	if timer.calls != 0 {
		t.Fatalf("a trigger character scheduled a debounce (%d timers)", timer.calls)
	}
	if len(msgs) == 0 {
		t.Fatal("a trigger character started no completion request")
	}
	for _, msg := range msgs {
		lt.feed(msg)
	}
	if lt.m.lsp.comp == nil {
		t.Fatalf("the trigger character opened no list (notice %q)", lt.m.notice)
	}
	if labels := completionLabels(lt); len(labels) != 1 || labels[0] != "alpha" {
		t.Fatalf("items = %v, want the server item", labels)
	}
}

func TestCompletionTriggerCharacterIsOnlyAdvertisedCharacters(t *testing.T) {
	timer := captureAutoTimer(t)
	port := newFakeLSP("go")
	lt := completionTestModel(t, port, []string{"foo"}, buffer.Pos{Line: 0, Col: 3})
	lt.m.lsp.port = &triggerLSP{fakeLSP: port, chars: []string{"."}}
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.SuggestOnTriggerCharacters = true

	// "x" is not advertised: it goes through the debounced word-character path.
	typeAutoChar(t, lt, 'x')
	if timer.calls != 1 {
		t.Fatalf("a non-trigger word character scheduled %d timers, want one", timer.calls)
	}
}

func TestCompletionTriggerCharactersOffFallsBackToDelay(t *testing.T) {
	timer := captureAutoTimer(t)
	port := newFakeLSP("go")
	lt := completionTestModel(t, port, []string{"foo"}, buffer.Pos{Line: 0, Col: 3})
	lt.m.lsp.port = &triggerLSP{fakeLSP: port, chars: []string{"."}}
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.SuggestOnTriggerCharacters = false

	typeAutoChar(t, lt, '.')
	if timer.calls != 0 || lt.m.lsp.comp != nil {
		t.Fatalf("with suggest_on_trigger_characters off: timers=%d comp=%v", timer.calls, lt.m.lsp.comp)
	}
}

func TestCompletionLiveFilterNarrowsWithoutAServerRequest(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{
		{Label: "alpha", Insert: "alpha"},
		{Label: "beta", Insert: "beta"},
	}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	counter := &countingLSP{fakeLSP: port}
	lt.m.lsp.port = counter

	feedCompletion(t, lt, lt.m.completionAsked())
	if labels := completionLabels(lt); len(labels) != 2 || labels[0] != "alpha" {
		t.Fatalf("items = %v, want the server items", labels)
	}
	if counter.completions != 1 {
		t.Fatalf("server calls = %d, want one", counter.completions)
	}

	// Typing "l" narrows the open list to items matching "al" with no new request.
	lt.feed(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if lt.m.lsp.comp == nil {
		t.Fatalf("live filtering closed the list (notice %q)", lt.m.notice)
	}
	if labels := completionLabels(lt); len(labels) != 1 || labels[0] != "alpha" {
		t.Fatalf("filtered items = %v, want only alpha", labels)
	}
	if counter.completions != 1 {
		t.Fatalf("live filtering asked the server again: %d calls", counter.completions)
	}

	// Accepting replaces the two typed characters, not just inserts at the caret.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyTab})
	edits := lt.ed.applied["/p/a.go"]
	if len(edits) != 1 {
		t.Fatalf("applied = %+v, want one edit", edits)
	}
	want := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 2}, Text: "alpha"}
	if edits[0] != want {
		t.Fatalf("applied edit = %+v, want %+v", edits[0], want)
	}
}

func TestCompletionLiveFilterEmptiedReasksOnce(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	counter := &countingLSP{fakeLSP: port}
	lt.m.lsp.port = counter

	feedCompletion(t, lt, lt.m.completionAsked())
	if counter.completions != 1 {
		t.Fatalf("server calls = %d, want one", counter.completions)
	}
	// "z" leaves no matching item: the list dismisses and asks once for the new
	// prefix (the server's answer may open it again).
	_, cmd := lt.m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	for _, msg := range deliver(cmd) {
		if msg != nil {
			lt.feed(msg)
		}
	}
	if counter.completions != 2 {
		t.Fatalf("server calls after an emptied filter = %d, want two", counter.completions)
	}
}

func TestCompletionLiveFilterNonWordDismisses(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	feedCompletion(t, lt, lt.m.completionAsked())
	if lt.m.lsp.comp == nil {
		t.Fatal("the list did not open")
	}
	lt.feed(tea.KeyPressMsg{Code: ' ', Text: " "})
	if lt.m.lsp.comp != nil {
		t.Fatal("a non-word character kept the completion list open")
	}
}

// TestCompletionBackspaceRefiltersAndKeepsSelection checks Backspace while the
// completion popup is open deletes one character, re-filters the rows by the shorter
// prefix without asking the server again, and keeps the highlighted candidate (by
// its former index, not its label).
func TestCompletionBackspaceRefiltersAndKeepsSelection(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{
		{Label: "alpha", Insert: "alpha"},
		{Label: "alpine", Insert: "alpine"},
		{Label: "beta", Insert: "beta"},
	}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	counter := &countingLSP{fakeLSP: port}
	lt.m.lsp.port = counter
	feedCompletion(t, lt, lt.m.completionAsked())
	if labels := completionLabels(lt); len(labels) != 3 {
		t.Fatalf("items = %v, want all server items", labels)
	}
	// Highlight the second candidate, then type a character that filters the list.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyDown})
	lt.feed(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if labels := completionLabels(lt); len(labels) != 2 || labels[0] != "alpha" || labels[1] != "alpine" {
		t.Fatalf("filtered items = %v, want alpha+alpine", labels)
	}
	if got := lt.m.lsp.comp.pop.Selected(); got != 1 {
		t.Fatalf("highlight after typing = %d, want the kept alpine row 1", got)
	}
	if counter.completions != 1 {
		t.Fatalf("typing asked the server again: %d calls", counter.completions)
	}

	// Backspace deletes the "l" and widens the filter back to every stored item that
	// starts with "a"; the highlight stays on alpine.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if lines := lt.ed.tabs[0].lines; lines[0] != "a" {
		t.Fatalf("document after Backspace = %q, want \"a\"", lines[0])
	}
	if lt.m.lsp.comp == nil {
		t.Fatalf("Backspace closed the completion list (notice %q)", lt.m.notice)
	}
	if labels := completionLabels(lt); len(labels) != 2 || labels[0] != "alpha" || labels[1] != "alpine" {
		t.Fatalf("items after Backspace = %v, want alpha+alpine", labels)
	}
	if got := lt.m.lsp.comp.pop.Selected(); got != 1 {
		t.Fatalf("highlight after Backspace = %d, want the kept alpine row 1", got)
	}
	if counter.completions != 1 {
		t.Fatalf("Backspace asked the server again: %d calls", counter.completions)
	}
	if edits := lt.ed.applied["/p/a.go"]; len(edits) != 0 {
		t.Fatalf("Backspace edited the document through the completion path: %+v", edits)
	}
	// Accepting now replaces the shorter word ("a") with the kept candidate, not the
	// two-character word the filter covered before Backspace.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyTab})
	edits := lt.ed.applied["/p/a.go"]
	want := editor.LSPEdit{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 1}, Text: "alpine"}
	if len(edits) != 1 || edits[0] != want {
		t.Fatalf("applied = %+v, want %+v", edits, want)
	}
}

// TestCompletionBackspaceRecapturesTheVersionGuard checks a re-filtered list captured
// the document version after the edit: accepting it passes that version to the editor's
// stale guard (the request version from before Backspace is not reused).
func TestCompletionBackspaceRecapturesTheVersionGuard(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}, {Label: "alpine", Insert: "alpine"}}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	lt.ed.version, lt.ed.versionOK = editor.Version{Gen: 1, Ver: 1}, true
	feedCompletion(t, lt, lt.m.completionAsked())
	lt.ed.version = editor.Version{Gen: 2, Ver: 2}
	lt.feed(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if lt.m.lsp.comp == nil {
		t.Fatalf("Backspace closed the list (notice %q)", lt.m.notice)
	}
	if !lt.m.lsp.comp.versionOK || lt.m.lsp.comp.version != lt.ed.version {
		t.Fatalf("refiltered list version = %+v/%v, want the post-edit %+v", lt.m.lsp.comp.version, lt.m.lsp.comp.versionOK, lt.ed.version)
	}
	lt.ed.applyErr = editor.ErrStale
	lt.feed(tea.KeyPressMsg{Code: tea.KeyTab})
	if len(lt.ed.applied) != 0 {
		t.Fatalf("applied = %+v after a stale accept, want nothing", lt.ed.applied)
	}
}

// TestCompletionRefilterSelectionFallsBackWhenFilteredOut checks a highlighted
// candidate that the shorter/longer prefix filters out falls back to the first row.
func TestCompletionRefilterSelectionFallsBackWhenFilteredOut(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{
		{Label: "alpha", Insert: "alpha"},
		{Label: "alpine", Insert: "alpine"},
		{Label: "beta", Insert: "beta"},
	}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	feedCompletion(t, lt, lt.m.completionAsked())
	// Highlight beta (the third row), then filter to the "al" rows: beta is gone.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyEnd})
	lt.feed(tea.KeyPressMsg{Code: 'l', Text: "l"})
	if got := lt.m.lsp.comp.pop.Selected(); got != 0 {
		t.Fatalf("highlight = %d, want the first row after the chosen one filtered out", got)
	}
	if labels := completionLabels(lt); len(labels) != 2 || labels[0] != "alpha" {
		t.Fatalf("items = %v", labels)
	}
}

// TestCompletionBackspaceRestoresFilteredOutCandidates checks the re-filter works
// from the complete candidate pool, not from the rows currently shown: with initial
// candidates apple/atom, typing "p" hides atom, and Backspace must bring it back
// (Fresh keeps completion_items and rebuilds from them).
func TestCompletionBackspaceRestoresFilteredOutCandidates(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{
		{Label: "apple", Insert: "apple"},
		{Label: "atom", Insert: "atom"},
	}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	counter := &countingLSP{fakeLSP: port}
	lt.m.lsp.port = counter
	feedCompletion(t, lt, lt.m.completionAsked())
	if labels := completionLabels(lt); len(labels) != 2 {
		t.Fatalf("initial items = %v, want apple and atom", labels)
	}
	if len(lt.m.lsp.comp.all) != 2 {
		t.Fatalf("candidate pool = %v, want the complete pool", lt.m.lsp.comp.all)
	}

	// "ap" matches apple only: atom is filtered out but must stay in the pool.
	lt.feed(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if labels := completionLabels(lt); len(labels) != 1 || labels[0] != "apple" {
		t.Fatalf("items after p = %v, want only apple", labels)
	}
	if len(lt.m.lsp.comp.all) != 2 {
		t.Fatalf("candidate pool was truncated by the filter: %v", lt.m.lsp.comp.all)
	}

	// Backspace shortens the word to "a": atom comes back from the pool, with no
	// new server request.
	lt.feed(tea.KeyPressMsg{Code: tea.KeyBackspace})
	labels := completionLabels(lt)
	if len(labels) != 2 || labels[0] != "apple" || labels[1] != "atom" {
		t.Fatalf("items after Backspace = %v, want apple and atom again", labels)
	}
	if counter.completions != 1 {
		t.Fatalf("Backspace asked the server again: %d calls", counter.completions)
	}
	if lines := lt.ed.tabs[0].lines; lines[0] != "a" {
		t.Fatalf("document after Backspace = %q, want \"a\"", lines[0])
	}
}
