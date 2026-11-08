package tui

import (
	"strings"

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

// completionAsked opens the local buffer-word list at once and starts the
// language-server request in a command. The two phases are deliberately split:
// the words are in-memory, so they appear on this update; the server may be slow
// and its items are merged later by completionReplied with the same identity. It is
// the action behind Trigger Completion / lsp_completion.
func (m *Model) completionAsked() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	path := docPath(doc)
	at := doc.Cursor()
	version, versioned := docVersion(doc)
	lang := m.lspLanguage(doc)
	// A large-file document is not LSP-served and must not be read here: its lines
	// come from disk. Buffer words are deferred for it rather than blocking Update.
	large := isLargeDoc(doc)
	lspWanted := !large && path != "" && m.lsp.enabled(lang)
	if lspWanted {
		tr := m.lsp.tracks[path]
		lspWanted = tr != nil && tr.state == lspOpen
	}
	port := m.lsp.port
	snapshot, wordsOK := m.wordSnapshot(doc)
	var wordItems []CompletionItem
	if wordsOK {
		wordItems = mergeBufferWordItems(nil, editor.BufferWordCompletions(snapshot))
	}
	wordsShown := len(wordItems) > 0
	if wordsShown {
		// First phase: the local list, before any server call. m.lspReply's
		// completion path is the shared popup opener, so accept and styling stay
		// the same as a server-only completion.
		_ = m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: path, at: at, items: wordItems})
	}
	if !lspWanted && !wordsShown {
		switch {
		case large && m.lsp.port != nil && m.lsp.enabled(lang):
			// The guard order for a large file is the LSP guard first: it is why the
			// server cannot serve the buffer, and the existing actions say so too.
			m.notice = "LSP: " + lspLargeReason(doc)
		case large:
			m.notice = "Buffer-word completion is not available in large files yet"
		default:
			m.notice = "No completions"
		}
		return nil
	}
	if !lspWanted {
		return nil
	}
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
	}, true
}
