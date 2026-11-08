package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// feedCompletion runs the completion command and lets the root handle its reply.
func feedCompletion(t *testing.T, lt *lspTest, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return // the local list was opened synchronously, or there is nothing to show
	}
	for _, msg := range deliver(cmd) {
		if _, next := lt.m.Update(msg); next != nil {
			for _, follow := range deliver(next) {
				lt.m.Update(follow)
			}
		}
	}
}

// countingLSP counts completion calls around a fake server so a test can prove the
// update path never waits for it.
type countingLSP struct {
	*fakeLSP
	completions int
}

func (c *countingLSP) Completion(path string, at buffer.Pos) ([]CompletionItem, error) {
	c.completions++
	return c.fakeLSP.Completion(path, at)
}

// completionLabels is the labels of the open completion popup.
func completionLabels(lt *lspTest) []string {
	items := lt.popup().Items()
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = item.Label
	}
	return out
}

func TestBufferWordCompletionWithoutLSPSuggestsWords(t *testing.T) {
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	lt := newLSPTest(t, nil, tab)

	// With no server there is no asynchronous phase: the list is open on return.
	if cmd := lt.m.completionAsked(); cmd != nil {
		t.Fatalf("a no-server completion returned a command: %T", cmd)
	}
	if lt.m.lsp.comp == nil {
		t.Fatal("no completion popup with no language server")
	}
	items := lt.popup().Items()
	if len(items) < 2 {
		t.Fatalf("items = %v, want buffer words", items)
	}
	if items[0].Label != "appetite" || items[1].Label != "apple" {
		t.Fatalf("items = %v, want appetite then apple (proximity)", completionLabels(lt))
	}
	if items[0].Detail != "Buffer Words" {
		t.Fatalf("detail = %q, want the Buffer Words source", items[0].Detail)
	}
	if lt.m.notice != "" {
		t.Fatalf("notice = %q, want no language-server refusal", lt.m.notice)
	}
}

func TestBufferWordCompletionMergesServerFirstAndDedupes(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{
		{Label: "application", Detail: "func()", Insert: "application"},
		{Label: "appetite", Detail: "server", Insert: "appetite"},
	}
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	lt := newLSPTest(t, port, tab)
	lt.open()

	cmd := lt.m.completionAsked()
	if cmd == nil {
		t.Fatal("a server completion returned no command")
	}
	// Phase one: the local words are on screen before the server is asked.
	local := completionLabels(lt)
	if len(local) != 2 || local[0] != "appetite" || local[1] != "apple" {
		t.Fatalf("local phase items = %v, want the buffer words at once", local)
	}
	feedCompletion(t, lt, cmd)
	labels := completionLabels(lt)
	if len(labels) != 3 {
		t.Fatalf("items = %v, want server items then one deduped word", labels)
	}
	if labels[0] != "application" || labels[1] != "appetite" {
		t.Fatalf("items = %v, want the server's items first", labels)
	}
	if labels[2] != "apple" {
		t.Fatalf("items = %v, want the buffer word after the server's", labels)
	}
	items := lt.popup().Items()
	if items[1].Detail != "server" {
		t.Fatalf("dedupe kept the wrong item: %+v", items[1])
	}
	if items[2].Detail != "Buffer Words" {
		t.Fatalf("buffer word detail = %q", items[2].Detail)
	}
}

func TestBufferWordCompletionDropsAStaleReply(t *testing.T) {
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "server-item", Insert: "server-item"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	cmd := lt.m.completionAsked()
	if cmd == nil {
		t.Fatal("no completion command")
	}
	// The user keeps typing while the server is in flight: the version changes,
	// and typing would close the local list; the reply must not merge into it.
	tab.lines[1] = "appl"
	tab.cursor = buffer.Pos{Line: 1, Col: 4}
	feedCompletion(t, lt, cmd)
	for _, item := range lt.m.lsp.comp.items {
		if item.Label == "server-item" {
			t.Fatalf("a stale server reply was merged: %+v", lt.m.lsp.comp.items)
		}
	}
}

func TestBufferWordCompletionDropsAReplyAfterTheCaretMoves(t *testing.T) {
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "server-item", Insert: "server-item"}}
	lt := newLSPTest(t, port, tab)
	lt.open()
	cmd := lt.m.completionAsked()
	// Move the caret without changing the text: the version is the same, the
	// completion would anchor to the old position.
	tab.cursor = buffer.Pos{Line: 0, Col: 2}
	feedCompletion(t, lt, cmd)
	for _, item := range lt.m.lsp.comp.items {
		if item.Label == "server-item" {
			t.Fatalf("a reply for the old caret was merged: %+v", lt.m.lsp.comp.items)
		}
	}
}

func TestBufferWordCompletionDefersLargeFiles(t *testing.T) {
	tab := &lspTab{
		path: "/p/a.go", lines: []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3}, large: true, size: 200 << 20,
	}
	lt := newLSPTest(t, nil, tab)
	tab.texts = 0 // the LSP sync of the test setup may have read the document
	if cmd := lt.m.completionAsked(); cmd != nil {
		t.Fatal("a large-file completion returned a command")
	}
	if tab.texts != 0 {
		t.Fatalf("large-file completion read the document %d times", tab.texts)
	}
	if !strings.Contains(lt.m.notice, "large files") {
		t.Fatalf("notice = %q, want the large-file explanation", lt.m.notice)
	}
}

func TestBufferWordCompletionEmptyPrefix(t *testing.T) {
	tab := &lspTab{
		path: "/p/a.go", lines: []string{"alpha"},
		cursor: buffer.Pos{Line: 0, Col: 0},
	}
	lt := newLSPTest(t, nil, tab)
	if cmd := lt.m.completionAsked(); cmd != nil {
		t.Fatal("an empty-prefix completion returned a command")
	}
	if lt.m.notice != "No completions" {
		t.Fatalf("notice = %q, want No completions", lt.m.notice)
	}
}

func TestMergeBufferWordItems(t *testing.T) {
	server := []CompletionItem{{Label: "apple", Insert: "apple"}}
	merged := mergeBufferWordItems(server, []editor.WordCandidate{
		{Text: "appetite"}, {Text: "apple"}, {Text: "Apple"},
	})
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want the server item and one deduped word", merged)
	}
	if merged[0].Label != "apple" || merged[0].Detail != "" {
		t.Fatalf("server item changed: %+v", merged[0])
	}
	if merged[1].Label != "appetite" || merged[1].Detail != editor.WordCompletionDetail || merged[1].Insert != "appetite" {
		t.Fatalf("word item = %+v", merged[1])
	}
}

// TestBufferWordCompletionShowsLocalWordsBeforeASlowServer is Lead's constraint:
// the update path must show the local candidates before the server answers, and the
// server phase must not call the port until its command runs.
func TestBufferWordCompletionShowsLocalWordsBeforeASlowServer(t *testing.T) {
	port := &countingLSP{fakeLSP: newFakeLSP("go")}
	port.items = []CompletionItem{{Label: "application", Insert: "application"}}
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	lt.m.lsp.port = port
	lt.m.lsp.tracks = map[string]*lspTrack{"/p/a.go": {path: "/p/a.go", lang: "go", state: lspOpen}}
	lt.m.lsp.stopped = false

	cmd := lt.m.completionAsked()
	if cmd == nil {
		t.Fatal("no server command")
	}
	if port.completions != 0 {
		t.Fatalf("the server was asked during Update (%d calls)", port.completions)
	}
	// The local phase is visible already: no waiting on the command.
	local := completionLabels(lt)
	if len(local) != 2 || local[0] != "appetite" {
		t.Fatalf("local phase items = %v, want the buffer words before the reply", local)
	}
	// The server phase runs later and merges its items above the local ones.
	feedCompletion(t, lt, cmd)
	if port.completions != 1 {
		t.Fatalf("server calls = %d, want one after the command ran", port.completions)
	}
	labels := completionLabels(lt)
	if len(labels) != 3 || labels[0] != "application" || labels[1] != "appetite" {
		t.Fatalf("merged items = %v, want the server item above the local words", labels)
	}
}

// TestBufferWordCompletionServerOnlyOpensOnTheReply checks a completion with no
// local words: there is nothing to show on Update, and the server's items open the
// list when they arrive.
func TestBufferWordCompletionServerOnlyOpensOnTheReply(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	tab := &lspTab{
		path: "/p/a.go", lines: []string{"alpha"},
		cursor: buffer.Pos{Line: 0, Col: 0}, // no prefix, so no buffer words
	}
	lt := newLSPTest(t, port, tab)
	lt.open()
	cmd := lt.m.completionAsked()
	if cmd == nil {
		t.Fatal("no server command")
	}
	if lt.m.lsp.comp != nil {
		t.Fatal("a server-only completion opened a local list")
	}
	feedCompletion(t, lt, cmd)
	if lt.m.lsp.comp == nil {
		t.Fatal("the server's items did not open the list")
	}
	if labels := completionLabels(lt); len(labels) != 1 || labels[0] != "alpha" {
		t.Fatalf("items = %v, want the server item", labels)
	}
}

// TestBufferWordCompletionClosedDuringServerWait checks that a local list the user
// closed is not resurrected by the late server reply.
func TestBufferWordCompletionClosedDuringServerWait(t *testing.T) {
	port := newFakeLSP("go")
	port.items = []CompletionItem{{Label: "alpha", Insert: "alpha"}}
	tab := &lspTab{
		path: "/p/a.go", lines: []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	lt := newLSPTest(t, port, tab)
	lt.open()
	cmd := lt.m.completionAsked()
	if cmd == nil || lt.m.lsp.comp == nil {
		t.Fatal("the local list did not open")
	}
	lt.m.closeCompletion(lt.m.lsp.comp)
	feedCompletion(t, lt, cmd)
	if lt.m.lsp.comp != nil || len(lt.m.overlays) != 0 {
		t.Fatalf("the closed list was resurrected: comp=%v overlays=%d", lt.m.lsp.comp, len(lt.m.overlays))
	}
}

func TestMergeServerIntoBufferItems(t *testing.T) {
	server := []CompletionItem{{Label: "apple", Insert: "apple"}, {Label: "beta", Insert: "beta"}}
	bufferItems := []CompletionItem{
		{Label: "apple", Insert: "apple", Detail: "Buffer Words"},
		{Label: "alpha", Insert: "alpha", Detail: "Buffer Words"},
	}
	merged := mergeServerIntoBufferItems(server, bufferItems)
	if len(merged) != 3 {
		t.Fatalf("merged = %+v, want the two server items and one buffer word", merged)
	}
	if merged[0].Label != "apple" || merged[1].Label != "beta" || merged[2].Label != "alpha" {
		t.Fatalf("order = %+v, want server items first", merged)
	}
	if merged[0].Detail != "" || merged[2].Detail != "Buffer Words" {
		t.Fatalf("details = %+v", merged)
	}
}

// TestTriggerCompletionShowsBufferWordsWithoutAServer is the route-level check:
// the palette/keyboard Show Completions action reaches the buffer-word phase even
// when no language server exists (the old path refused with a notice). It is
// skipped until the lsp.go route is wired; the implementation files are already in
// place and the setup mirrors TestBufferWordCompletionWithoutLSPSuggestsWords.
func TestTriggerCompletionShowsBufferWordsWithoutAServer(t *testing.T) {
	tab := &lspTab{
		path:   "/p/a.go",
		lines:  []string{"appetite apple", "app"},
		cursor: buffer.Pos{Line: 1, Col: 3},
	}
	lt := newLSPTest(t, nil, tab)
	lt.open()

	for _, msg := range lt.act("lsp_completion") {
		lt.feed(msg)
	}
	if lt.m.lsp.comp == nil {
		t.Fatalf("Show Completions without a server opened no list (notice %q)", lt.m.notice)
	}
	labels := completionLabels(lt)
	if len(labels) < 2 || labels[0] != "appetite" {
		t.Fatalf("items = %v, want buffer words", labels)
	}
}

// captureAutoTimer replaces the debounce command for one test: it records the
// requested delay and turns the scheduled message into an immediately deliverable
// command instead of waiting on tea.Tick.
func captureAutoTimer(t *testing.T) *capturedAutoTimer {
	t.Helper()
	c := &capturedAutoTimer{prev: completionAutoTimer}
	completionAutoTimer = func(delay time.Duration, msg completionAutoMsg) tea.Cmd {
		c.delay, c.msg, c.calls = delay, msg, c.calls+1
		return func() tea.Msg { return msg }
	}
	t.Cleanup(func() { completionAutoTimer = c.prev })
	return c
}

type capturedAutoTimer struct {
	prev  func(time.Duration, completionAutoMsg) tea.Cmd
	delay time.Duration
	msg   completionAutoMsg
	calls int
}

// typeAutoChar types one printable rune through the update path and returns the
// messages the update started (including the fake timer message).
func typeAutoChar(t *testing.T, lt *lspTest, r rune) []tea.Msg {
	t.Helper()
	_, cmd := lt.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	return deliver(cmd)
}

func autoCompletionTest(t *testing.T, lines []string, cursor buffer.Pos) *lspTest {
	t.Helper()
	tab := &lspTab{path: "/p/a.go", lines: lines, cursor: cursor}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	lt.m.view.opts.CompletionPopupAutoShow = true
	return lt
}

func TestAutoCompletionSchedulesAndOpensAfterTheDelay(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	lt.m.view.opts.QuickSuggestionsDelayMS = 25

	msgs := typeAutoChar(t, lt, 'e')
	if timer.calls != 1 || timer.delay != 25*time.Millisecond {
		t.Fatalf("timer calls=%d delay=%v, want one at the configured 25ms", timer.calls, timer.delay)
	}
	if len(msgs) != 1 {
		t.Fatalf("messages = %v, want the timer message", msgs)
	}
	lt.feed(msgs[0])
	if lt.m.lsp.comp == nil {
		t.Fatalf("the auto timer opened no completion list (notice %q)", lt.m.notice)
	}
	if labels := completionLabels(lt); len(labels) == 0 || labels[0] != "appetite" {
		t.Fatalf("items = %v, want the buffer word", labels)
	}
}

func TestAutoCompletionDisabledSchedulesNothing(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	lt.m.view.opts.CompletionPopupAutoShow = false
	typeAutoChar(t, lt, 'e')
	if timer.calls != 0 || lt.m.lsp.comp != nil {
		t.Fatalf("auto-show off: calls=%d comp=%v", timer.calls, lt.m.lsp.comp)
	}
}

func TestAutoCompletionQuickSuggestionsOffSchedulesNothing(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	lt.m.view.opts.QuickSuggestions = false
	typeAutoChar(t, lt, 'e')
	if timer.calls != 0 {
		t.Fatalf("quick_suggestions off scheduled %d timers", timer.calls)
	}
}

func TestAutoCompletionNonWordCharacterCancels(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	typeAutoChar(t, lt, 'e') // schedules
	old := timer.msg
	typeAutoChar(t, lt, ' ') // punct/space cancels, no new timer
	if timer.calls != 1 {
		t.Fatalf("timers = %d, want only the word-character one", timer.calls)
	}
	lt.feed(old)
	if lt.m.lsp.comp != nil {
		t.Fatal("a cancelled timer opened the list")
	}
}

func TestAutoCompletionStaleTimerAfterAnotherKeystroke(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	typeAutoChar(t, lt, 'e')
	old := timer.msg
	msgs := typeAutoChar(t, lt, 't') // a newer keystroke reschedules; old is stale
	if timer.calls != 2 {
		t.Fatalf("timers = %d, want two", timer.calls)
	}
	newer := timer.msg
	lt.feed(old)
	if lt.m.lsp.comp != nil {
		t.Fatal("the stale timer opened the list")
	}
	lt.feed(msgs[0])
	if lt.m.lsp.comp == nil {
		t.Fatal("the newest timer did not open the list")
	}
	_ = newer
}

func TestAutoCompletionTimerDroppedWhenTextChanged(t *testing.T) {
	timer := captureAutoTimer(t)
	tab := &lspTab{path: "/p/a.go", lines: []string{"appetite apple", "app"}, cursor: buffer.Pos{Line: 1, Col: 3}}
	lt := newLSPTest(t, nil, tab)
	lt.open()
	lt.m.view.opts.CompletionPopupAutoShow = true
	msgs := typeAutoChar(t, lt, 'e')
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	// Another edit changes the version before the timer fires.
	tab.lines[0] = "appetite apple pie"
	lt.feed(msgs[0])
	if lt.m.lsp.comp != nil {
		t.Fatal("the timer opened a list for an older document version")
	}
	_ = timer
}

func TestAutoCompletionClosedListDoesNotReopen(t *testing.T) {
	captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"appetite apple", "app"}, buffer.Pos{Line: 1, Col: 3})
	// Open the list from an auto timer, then dismiss it. A second pending timer
	// must not resurrect it.
	msgs := typeAutoChar(t, lt, 'e')
	lt.feed(msgs[0])
	if lt.m.lsp.comp == nil {
		t.Fatal("the list did not open")
	}
	lt.act("completion_dismiss")
	if lt.m.lsp.comp != nil {
		t.Fatal("completion_dismiss left the list open")
	}
	msgs = typeAutoChar(t, lt, 't') // schedules a fresh timer
	msgs2 := typeAutoChar(t, lt, 'i')
	_ = msgs2
	lt.feed(msgs[0])
	if lt.m.lsp.comp != nil {
		t.Fatal("a dismissed list was reopened by an older timer")
	}
}

func TestAutoCompletionNoPopupWhenEmptyPrefix(t *testing.T) {
	timer := captureAutoTimer(t)
	lt := autoCompletionTest(t, []string{"alpha"}, buffer.Pos{Line: 0, Col: 0})
	typeAutoChar(t, lt, ' ')
	typeAutoChar(t, lt, '.')
	if timer.calls != 0 {
		t.Fatalf("non-word characters scheduled %d timers", timer.calls)
	}
}

func TestCompletionMultiBufferWordsInThePopup(t *testing.T) {
	active := &lspTab{path: "/p/a.go", lines: []string{"alpine", "al"}, cursor: buffer.Pos{Line: 1, Col: 2}}
	other := &lspTab{path: "/p/b.txt", lines: []string{"alphabet soup"}}
	lt := newLSPTest(t, nil, active, other)
	lt.open() // the first tab is active; the second is the other buffer

	feedCompletion(t, lt, lt.m.completionAsked())
	labels := completionLabels(lt)
	foundActive, foundOther := false, false
	for i, label := range labels {
		if label == "alpine" {
			foundActive = true
		}
		if label == "alphabet" {
			foundOther = true
			if !foundActive {
				t.Fatalf("other-buffer word %q ranked before the active word: %v", label, labels)
			}
			_ = i
		}
	}
	if !foundActive || !foundOther {
		t.Fatalf("items = %v, want the active and the other buffer's words", labels)
	}
}

// TestCompletionSettingsApplyLive checks the four Completion rows' live-apply
// plumbing: each key copies its own option and leaves the others alone.
func TestCompletionSettingsApplyLive(t *testing.T) {
	t.Parallel()
	base := DefaultViewOptions()
	next := base
	next.CompletionPopupAutoShow = true
	next.QuickSuggestions = false
	next.QuickSuggestionsDelayMS = 25
	next.SuggestOnTriggerCharacters = false
	tests := []struct {
		key   string
		check func(ViewOptions) bool
	}{
		{"editor.completion_popup_auto_show", func(o ViewOptions) bool { return o.CompletionPopupAutoShow }},
		{"editor.quick_suggestions", func(o ViewOptions) bool { return !o.QuickSuggestions }},
		{"editor.quick_suggestions_delay_ms", func(o ViewOptions) bool { return o.QuickSuggestionsDelayMS == 25 }},
		{"editor.suggest_on_trigger_characters", func(o ViewOptions) bool { return !o.SuggestOnTriggerCharacters }},
	}
	for _, tt := range tests {
		got := copyViewOption(base, next, tt.key)
		if !tt.check(got) {
			t.Fatalf("%s did not apply its option", tt.key)
		}
	}
	// One key at a time, not the whole struct.
	got := copyViewOption(base, next, "editor.completion_popup_auto_show")
	if !got.CompletionPopupAutoShow || got.QuickSuggestions != base.QuickSuggestions ||
		got.QuickSuggestionsDelayMS != base.QuickSuggestionsDelayMS ||
		got.SuggestOnTriggerCharacters != base.SuggestOnTriggerCharacters {
		t.Fatalf("copy = %+v, want only completion_popup_auto_show taken", got)
	}
}
