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
