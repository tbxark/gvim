package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// recordingTriggerLSP records the completion request positions and advertises trigger
// characters, so a test can prove a non-word trigger starts a NEW request at the new
// caret.
type recordingTriggerLSP struct {
	*fakeLSP
	positions []buffer.Pos
	chars     []string
}

func (r *recordingTriggerLSP) Completion(path string, at buffer.Pos) ([]CompletionItem, error) {
	r.positions = append(r.positions, at)
	return r.fakeLSP.Completion(path, at)
}

func (r *recordingTriggerLSP) CompletionTriggerCharacters(string) []string { return r.chars }

// TestCompletionNonWordClosesAndInserts is the Fresh e2e behavior for non-word
// characters: they are keys the popup is not in the way of, so the list closes and the
// character goes to the editor. Fresh's own tests cover space, semicolon, open paren,
// equals, close brace, comma, plus, close paren and dot; the same table runs here.
func TestCompletionNonWordClosesAndInserts(t *testing.T) {
	cases := []struct {
		name string
		code rune
		text string
	}{
		{"space", ' ', " "},
		{"semicolon", ';', ";"},
		{"open paren", '(', "("},
		{"equals", '=', "="},
		{"close brace", '}', "}"},
		{"comma", ',', ","},
		{"plus", '+', "+"},
		{"close paren", ')', ")"},
		{"dot", '.', "."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			port := newFakeLSP("go")
			port.items = []CompletionItem{{Label: "calculate", Insert: "calculate"}}
			lt := completionTestModel(t, port, []string{"calc"}, buffer.Pos{Line: 0, Col: 4})
			counter := &countingLSP{fakeLSP: port}
			lt.m.lsp.port = counter
			feedCompletion(t, lt, lt.m.completionAsked())
			if lt.m.lsp.comp == nil {
				t.Fatal("the completion list did not open")
			}
			_, cmd := lt.m.Update(tea.KeyPressMsg{Code: tc.code, Text: tc.text})
			for _, msg := range deliver(cmd) {
				lt.m.Update(msg)
			}
			if lt.m.lsp.comp != nil {
				t.Fatalf("%s kept the completion list open", tc.name)
			}
			if got := lt.ed.tabs[0].lines[0]; got != "calc"+tc.text {
				t.Fatalf("document = %q, want %q", got, "calc"+tc.text)
			}
			if counter.completions != 1 {
				t.Fatalf("%s asked the server again: %d calls", tc.name, counter.completions)
			}
			if lt.m.notice != "" {
				t.Fatalf("notice = %q", lt.m.notice)
			}
		})
	}
}

// TestCompletionNonWordTriggerReopensNewList checks the trigger-character nuance: a
// non-word character the servers advertise as a completion trigger closes the old list,
// is inserted, and (with completion_popup_auto_show + suggest_on_trigger_characters on)
// immediately starts a NEW request at the new caret. It is a fresh list, not an in-place
// re-filter: the selected row starts at the first row from the new request, and the new
// request carries the post-insert document version (Fresh closes the list and asks
// again; it does not filter non-word characters).
func TestCompletionNonWordTriggerReopensNewList(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "calculate", Insert: "calculate"}, {Label: "callee", Insert: "callee"}}
	lt := completionTestModel(t, port, []string{"calc"}, buffer.Pos{Line: 0, Col: 4})
	rec := &recordingTriggerLSP{fakeLSP: port, chars: []string{"."}}
	lt.m.lsp.port = rec
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.SuggestOnTriggerCharacters = true
	lt.ed.version, lt.ed.versionOK = editor.Version{Gen: 3, Ver: 7}, true

	feedCompletion(t, lt, lt.m.completionAsked())
	if lt.m.lsp.comp == nil {
		t.Fatal("the first completion list did not open")
	}
	if len(rec.positions) != 1 || rec.positions[0] != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("first request positions = %v", rec.positions)
	}
	first := lt.m.lsp.comp

	_, cmd := lt.m.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	for _, msg := range deliver(cmd) {
		if _, next := lt.m.Update(msg); next != nil {
			for _, follow := range deliver(next) {
				lt.m.Update(follow)
			}
		}
	}
	if got := lt.ed.tabs[0].lines[0]; got != "calc." {
		t.Fatalf("document = %q, want the inserted dot", got)
	}
	if lt.m.lsp.comp == nil {
		t.Fatalf("the trigger character opened no new list (notice %q)", lt.m.notice)
	}
	if lt.m.lsp.comp == first {
		t.Fatal("the trigger character filtered the old list instead of closing it and asking again")
	}
	if len(rec.positions) != 2 || rec.positions[1] != (buffer.Pos{Line: 0, Col: 5}) {
		t.Fatalf("requests = %v, want a second one at the new caret", rec.positions)
	}
	if !lt.m.lsp.comp.versionOK || lt.m.lsp.comp.version != lt.ed.version {
		t.Fatalf("new list version = %+v/%v, want the post-insert %+v", lt.m.lsp.comp.version, lt.m.lsp.comp.versionOK, lt.ed.version)
	}
	if got := lt.m.lsp.comp.pop.Selected(); got != 0 {
		t.Fatalf("new list highlight = %d, want the first row of the new request", got)
	}
}

// TestCompletionNonWordTriggerOffDoesNotRequest checks suggest_on_trigger_characters off:
// the non-word trigger still closes and inserts, but starts no new completion request
// (Fresh's maybe_trigger_completion checks the option).
func TestCompletionNonWordTriggerOffDoesNotRequest(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "calculate", Insert: "calculate"}}
	lt := completionTestModel(t, port, []string{"calc"}, buffer.Pos{Line: 0, Col: 4})
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.SuggestOnTriggerCharacters = false
	rec := &recordingTriggerLSP{fakeLSP: port, chars: []string{"."}}
	lt.m.lsp.port = rec
	feedCompletion(t, lt, lt.m.completionAsked())
	if lt.m.lsp.comp == nil {
		t.Fatal("the completion list did not open")
	}
	_, cmd := lt.m.Update(tea.KeyPressMsg{Code: '.', Text: "."})
	for _, msg := range deliver(cmd) {
		lt.m.Update(msg)
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("a non-trigger invocation kept the list open")
	}
	if got := lt.ed.tabs[0].lines[0]; got != "calc." {
		t.Fatalf("document = %q, want the inserted dot", got)
	}
	if len(rec.positions) != 1 {
		t.Fatalf("requests = %v, want no new request when suggest_on_trigger_characters is off", rec.positions)
	}
}

// TestCompletionNonWordCancelsPendingAutoPopup checks a non-word, non-trigger character
// typed while the list is open cancels a pending debounced auto-popup: the captured timer
// message no longer opens a list or asks the server.
func TestCompletionNonWordCancelsPendingAutoPopup(t *testing.T) {
	timer := captureAutoTimer(t)
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "apple", Insert: "apple"}}
	lt := completionTestModel(t, port, []string{"a"}, buffer.Pos{Line: 0, Col: 1})
	counter := &countingLSP{fakeLSP: port}
	lt.m.lsp.port = counter
	lt.m.view.opts.CompletionPopupAutoShow = true
	lt.m.view.opts.QuickSuggestions = true
	feedCompletion(t, lt, lt.m.completionAsked())
	if lt.m.lsp.comp == nil {
		t.Fatal("the completion list did not open")
	}
	pending := lt.m.afterTypedCompletionText("b")
	if timer.calls != 1 {
		t.Fatalf("timer calls = %d, want the pending debounce", timer.calls)
	}
	lt.feed(tea.KeyPressMsg{Code: ';', Text: ";"})
	if lt.m.lsp.comp != nil {
		t.Fatal("a non-word character kept the list open")
	}
	// Deliver the timer that was pending before the character: its sequence is stale,
	// so it must not reopen anything.
	for _, msg := range deliver(pending) {
		lt.m.Update(msg)
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("the cancelled auto-popup timer reopened a list")
	}
	if counter.completions != 1 {
		t.Fatalf("server calls = %d, want only the initial one", counter.completions)
	}
	if timer.calls != 1 {
		t.Fatalf("a non-word character scheduled another timer: %d calls", timer.calls)
	}
	if got := lt.ed.tabs[0].lines[0]; got != "a;" {
		t.Fatalf("document = %q, want a;", got)
	}
}
