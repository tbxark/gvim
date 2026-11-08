package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// Buffer-word completion: Trigger Completion offers the words already in the
// buffer when no language server can answer, and merges them after the server's
// items when one can. This is the TUI half of Fresh's buffer_words provider
// (services/completion/buffer_words.rs): the update goroutine captures a bounded
// line window around the caret (no file I/O; large-file documents are skipped
// because reading their lines would hit the disk), the scan and the server call
// run in one command, and the reply is merged here.
//
// The server is what may take time: the command never runs on the update
// goroutine, and a reply is discarded when the document changed or the caret moved
// while it was in flight, so typing during a slow completion stays responsive.

// maxWordSnapshotLines bounds the lines one snapshot keeps even in a file of very
// short lines.
const maxWordSnapshotLines = 20_000

// completionReplyMsg is the server half of a completion request. The buffer words
// were already shown when the request started (completionAsked opens them at once);
// this reply only adds the server's items, so a slow language server can never make
// the local list wait. The identity fields make a reply discardable: a reply for
// another document, an older version or a moved caret is dropped.
type completionReplyMsg struct {
	path      string
	at        buffer.Pos
	version   uint64
	versioned bool
	server    []CompletionItem
	err       error
	// wordsShown says the local list was opened before the request. If it has been
	// closed by the time the reply arrives, the user dismissed it and the server's
	// items must not resurrect it.
	wordsShown bool
}

// completionSources is the state one completion request is built from, captured
// on the update goroutine so the command and the auto-popup timer never read the
// document off that goroutine.
type completionSources struct {
	path      string
	at        buffer.Pos
	version   uint64
	versioned bool
	lang      string
	large     bool
	lspWanted bool
	port      LSPSession
	snapshot  editor.WordSnapshot
	wordsOK   bool
}

// completionSourcesFor captures what Trigger Completion would use for doc.
func (m *Model) completionSourcesFor(doc Document) completionSources {
	s := completionSources{
		path: docPath(doc), at: doc.Cursor(), lang: m.lspLanguage(doc),
	}
	s.version, s.versioned = docVersion(doc)
	s.large = isLargeDoc(doc)
	s.lspWanted = !s.large && s.path != "" && m.lsp.enabled(s.lang)
	if s.lspWanted {
		tr := m.lsp.tracks[s.path]
		s.lspWanted = tr != nil && tr.state == lspOpen
	}
	s.port = m.lsp.port
	s.snapshot, s.wordsOK = m.wordSnapshot(doc)
	return s
}

// completionAsked opens the local buffer-word list at once and starts the
// language-server request in a command. The two phases are deliberately split:
// the words are in-memory, so they appear on this update; the server may be slow
// and its items are merged later by completionReplied with the same identity. It is
// the action behind Trigger Completion / lsp_completion, and the auto-popup timer
// calls it too.
func (m *Model) completionAsked() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	src := m.completionSourcesFor(doc)
	var wordItems []CompletionItem
	if src.wordsOK {
		wordItems = mergeBufferWordItems(nil, editor.BufferWordCompletions(src.snapshot))
	}
	wordsShown := len(wordItems) > 0
	if wordsShown {
		// First phase: the local list, before any server call. m.lspReply's
		// completion path is the shared popup opener, so accept and styling stay
		// the same as a server-only completion.
		_ = m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: src.path, at: src.at, items: wordItems})
	}
	if !src.lspWanted && !wordsShown {
		switch {
		case src.large && m.lsp.port != nil && m.lsp.enabled(src.lang):
			// The guard order for a large file is the LSP guard first: it is why the
			// server cannot serve the buffer, and the existing actions say so too.
			m.notice = "LSP: " + lspLargeReason(doc)
		case src.large:
			m.notice = "Buffer-word completion is not available in large files yet"
		default:
			m.notice = "No completions"
		}
		return nil
	}
	if !src.lspWanted {
		return nil
	}
	path, at, version, versioned := src.path, src.at, src.version, src.versioned
	port := src.port
	return func() tea.Msg {
		r := completionReplyMsg{
			path: path, at: at, version: version, versioned: versioned, wordsShown: wordsShown,
		}
		if port != nil {
			r.server, r.err = port.Completion(path, at)
		}
		return r
	}
}

// completionAutoMsg is one due auto-popup timer. The identity fields make a timer
// from an older keystroke or an older document version discardable.
type completionAutoMsg struct {
	seq       uint64
	path      string
	at        buffer.Pos
	version   uint64
	versioned bool
}

// completionAutoTimer is the debounce command; tests replace it to capture the
// requested delay and fire the message deterministically.
var completionAutoTimer = func(delay time.Duration, msg completionAutoMsg) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg { return msg })
}

// afterTypedCompletionText schedules the auto-popup after a typed printable rune,
// or cancels a pending one. It runs on the update goroutine after the character was
// inserted, does no I/O, and only schedules a tea command; the popup itself opens
// when the timer message comes back. Word characters debounce by
// editor.quick_suggestions_delay_ms; any other character cancels.
func (m *Model) afterTypedCompletionText(text string) tea.Cmd {
	m.view.completionAutoSeq++
	if !m.view.opts.CompletionPopupAutoShow {
		return nil
	}
	doc := m.ed.Active()
	if doc == nil || isLargeDoc(doc) {
		return nil
	}
	r, ok := singleRune(text)
	if !ok {
		return nil
	}
	// A character the running server advertises as a completion trigger opens the
	// list immediately, with no debounce (Fresh's suggest_on_trigger_characters).
	if m.view.opts.SuggestOnTriggerCharacters && m.completionTriggeredLocally(doc, r) {
		return m.completionAsked()
	}
	if !m.view.opts.QuickSuggestions {
		return nil
	}
	if !editor.IsWordRune(r, m.docLanguageSettings(doc).WordCharacters) {
		return nil
	}
	at := doc.Cursor()
	prefix := editor.CompletionPrefix(doc.Line(at.Line), at.Col, m.docLanguageSettings(doc).WordCharacters)
	if prefix == "" {
		return nil
	}
	delay := m.view.opts.QuickSuggestionsDelayMS
	if delay < 0 {
		delay = 0
	}
	version, versioned := docVersion(doc)
	msg := completionAutoMsg{
		seq: m.view.completionAutoSeq, path: docPath(doc), at: at,
		version: version, versioned: versioned,
	}
	return completionAutoTimer(time.Duration(delay)*time.Millisecond, msg)
}

// cancelAutoCompletion invalidates the pending auto-popup trigger. The completion
// list's close path calls it, so Esc/dismiss/typing cannot reopen a list the user
// closed.
func (m *Model) cancelAutoCompletion() {
	m.view.completionAutoSeq++
}

// autoCompletionDue opens the auto-popup when its timer expires and it still
// belongs to the newest keystroke, the same document version and the same caret.
func (m *Model) autoCompletionDue(msg completionAutoMsg) tea.Cmd {
	if msg.seq != m.view.completionAutoSeq {
		return nil
	}
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != msg.path || doc.Cursor() != msg.at {
		m.cancelAutoCompletion()
		return nil
	}
	if version, ok := docVersion(doc); msg.versioned && (!ok || version != msg.version) {
		m.cancelAutoCompletion()
		return nil
	}
	if m.lsp.comp != nil || len(m.overlays) > 0 {
		return nil // another list or dialog is up
	}
	src := m.completionSourcesFor(doc)
	if src.snapshot.Prefix == "" || (!src.wordsOK && !src.lspWanted) {
		return nil
	}
	return m.completionAsked()
}

// completionReplied merges the server half into the open local list (or opens it
// when no buffer words were shown). The reply is dropped when the active document,
// its version or the caret changed, and when the user already closed the local
// list; a slow server can therefore neither block nor resurrect a completion.
func (m *Model) completionReplied(r completionReplyMsg) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil || docPath(doc) != r.path {
		return nil
	}
	if version, ok := docVersion(doc); r.versioned && (!ok || version != r.version) {
		return nil
	}
	if doc.Cursor() != r.at {
		return nil
	}
	current := m.lsp.comp
	if current != nil {
		if current.path != r.path || current.at != r.at {
			return nil
		}
		merged := mergeServerIntoBufferItems(r.server, current.items)
		if r.err != nil {
			m.notice = r.err.Error()
			if len(merged) == 0 {
				m.closeCompletion(current)
				return nil
			}
		}
		if len(merged) == len(current.items) && len(r.server) == 0 {
			return nil // nothing new: leave the open local list as it is
		}
		// Replace the open list with the merged one: the popup is still shown, only
		// its rows change, so the user sees the remote items appear in place.
		m.closeCompletion(current)
		return m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: r.path, at: r.at, items: merged})
	}
	if r.wordsShown {
		return nil // the local list was closed while the server worked: do not reopen
	}
	if r.err != nil {
		m.notice = r.err.Error()
		if len(r.server) == 0 {
			return nil
		}
	}
	if len(r.server) == 0 {
		return nil
	}
	return m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: r.path, at: r.at, items: r.server})
}

// mergeBufferWordItems puts the server's items first and appends the buffer-word
// candidates that the server did not already offer (a case-insensitive comparison
// of the inserted text), marked with the Buffer Words detail.
func mergeBufferWordItems(server []CompletionItem, words []editor.WordCandidate) []CompletionItem {
	items := append([]CompletionItem(nil), server...)
	seen := make(map[string]bool, len(items)+len(words))
	for _, item := range items {
		seen[completionInsertKey(item)] = true
	}
	for _, word := range words {
		key := strings.ToLower(word.Text)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, CompletionItem{
			Label: word.Text, Detail: editor.WordCompletionDetail, Insert: word.Text,
		})
	}
	return items
}

// mergeServerIntoBufferItems puts the server's items first and keeps the
// buffer-word items that the server did not already offer. It is the second half
// of a two-phase completion: the list was showing the local words, and this makes
// the server's rows appear above them.
func mergeServerIntoBufferItems(server, bufferItems []CompletionItem) []CompletionItem {
	seen := make(map[string]bool, len(server)+len(bufferItems))
	merged := make([]CompletionItem, 0, len(server)+len(bufferItems))
	for _, item := range server {
		key := completionInsertKey(item)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	for _, item := range bufferItems {
		key := completionInsertKey(item)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, item)
	}
	return merged
}

// completionInsertKey is the text a completion inserts, lowercased: what dedupes a
// server item from a buffer word.
func completionInsertKey(item CompletionItem) string {
	text := item.Insert
	if text == "" {
		text = item.Label
	}
	return strings.ToLower(text)
}

// wordSnapshot captures the bounded line window the buffer-word scan reads. It runs
// on the update goroutine and only reads the in-memory document; a large-file
// document is left to the caller (the caller skips it) because its lines are read
// from disk.
func (m *Model) wordSnapshot(doc Document) (editor.WordSnapshot, bool) {
	if isLargeDoc(doc) {
		return editor.WordSnapshot{}, false
	}
	count := doc.LineCount()
	if count <= 0 {
		return editor.WordSnapshot{}, false
	}
	at := doc.Cursor()
	cursorLine := at.Line
	if cursorLine < 0 {
		cursorLine = 0
	}
	if cursorLine >= count {
		cursorLine = count - 1
	}
	wordChars := m.docLanguageSettings(doc).WordCharacters
	prefix := editor.CompletionPrefix(doc.Line(cursorLine), at.Col, wordChars)
	if prefix == "" {
		return editor.WordSnapshot{}, false
	}
	// Grow the window in both directions from the caret until the byte budget is
	// spent, always keeping the caret line, then read the lines.
	start, end := cursorLine, cursorLine
	spent := len(doc.Line(cursorLine)) + 1
	radius := editor.WordScanBytes
	for start > 0 || end+1 < count {
		if end+1 < count && spent < radius && end-start+1 < maxWordSnapshotLines {
			end++
			spent += len(doc.Line(end)) + 1
		}
		if start > 0 && spent < radius && end-start+1 < maxWordSnapshotLines {
			start--
			spent += len(doc.Line(start)) + 1
		}
		if spent >= radius || end-start+1 >= maxWordSnapshotLines {
			break
		}
	}
	lines := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		lines = append(lines, doc.Line(i))
	}
	top := doc.Top()
	bottom := top + max(m.layout().BodyBottom-m.layout().BodyTop, 0)
	if bottom < top {
		bottom = top
	}
	return editor.WordSnapshot{
		Lines: lines, StartLine: start, CursorLine: cursorLine, CursorCol: at.Col,
		Prefix: prefix, WordChars: wordChars, PrefixHasUpper: editor.HasUpper(prefix),
		ViewportTop: top, ViewportBottom: bottom,
		Other: m.otherWordBuffers(),
	}, true
}

// otherWordBuffers captures a bounded word window from every other open tab, in tab
// order (Fresh uses MRU order; gvim's tabs are ordered, so this is the documented
// residual). The windows are read on the update goroutine like the active one: no
// disk I/O, large-file documents skipped, and at most editor.OtherWordBufferLimit
// buffers with editor.OtherWordBufferBytes each.
func (m *Model) otherWordBuffers() []editor.WordBuffer {
	tabs, ok := m.ed.(tabDocs)
	if !ok {
		return nil
	}
	infos := m.ed.Tabs()
	active := m.activeTab()
	out := make([]editor.WordBuffer, 0, editor.OtherWordBufferLimit)
	for i := range infos {
		if i == active {
			continue
		}
		if len(out) >= editor.OtherWordBufferLimit {
			break
		}
		doc := tabs.DocAt(i)
		if doc == nil || isLargeDoc(doc) {
			continue
		}
		if lines := middleLineWindow(doc, editor.OtherWordBufferBytes); len(lines) > 0 {
			out = append(out, editor.WordBuffer{Lines: lines})
		}
	}
	return out
}

// middleLineWindow reads a bounded line window from the middle of doc: Fresh's
// other-buffer scan takes 64 KiB from the middle of the buffer rather than from its
// caret.
func middleLineWindow(doc Document, radius int) []string {
	count := doc.LineCount()
	if count <= 0 {
		return nil
	}
	mid := count / 2
	start, end := mid, mid
	spent := len(doc.Line(mid)) + 1
	for start > 0 || end+1 < count {
		if end+1 < count && spent < radius {
			end++
			spent += len(doc.Line(end)) + 1
		}
		if start > 0 && spent < radius {
			start--
			spent += len(doc.Line(start)) + 1
		}
		if spent >= radius {
			break
		}
	}
	lines := make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		lines = append(lines, doc.Line(i))
	}
	return lines
}
