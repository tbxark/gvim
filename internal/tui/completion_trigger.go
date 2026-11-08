package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// Completion trigger characters and the live filter of an open completion list.
//
// Fresh (app/lsp_requests.rs, app/popup_actions.rs) opens the popup immediately
// after a character the server advertises as a completion trigger, and while the
// popup is open it inserts further typed characters and rebuilds the rows filtered
// by the longer word prefix instead of re-asking the server. This file is the TUI
// half of both: the capability accessor is an optional LSPSession part, and the
// filter runs on the items already held by the open list.

// completionTriggerSource is the optional LSPSession part that exposes the servers'
// completion trigger characters for an open document. A session without it (or a
// fake in a test) auto-opens completion only on word characters.
type completionTriggerSource interface {
	CompletionTriggerCharacters(path string) []string
}

// completionTriggerCharacters returns the union of the trigger characters the
// running servers advertise for doc, or nil. It reads the capability snapshot, so
// it is safe on the update goroutine.
func (m *Model) completionTriggerCharacters(doc Document) []string {
	path := docPath(doc)
	if path == "" || m.lsp.port == nil {
		return nil
	}
	if src, ok := m.lsp.port.(completionTriggerSource); ok {
		return src.CompletionTriggerCharacters(path)
	}
	return nil
}

// completionTriggeredLocally reports whether r is one of doc's advertised trigger
// characters.
func (m *Model) completionTriggeredLocally(doc Document, r rune) bool {
	for _, ch := range m.completionTriggerCharacters(doc) {
		if ch == string(r) {
			return true
		}
	}
	return false
}

// completionTyped inserts a printable word character into the document and
// live-filters the open completion list by the now-longer word prefix, as Fresh
// does. It reports false for input the list should not handle (not a word
// character, another document), so lspKey falls back to today's dismissal; on a
// successful filter the list stays open. A filter that empties closes the list and
// asks again once. The inserted characters are a normal editor edit, so the next
// reply's document-version guard still drops anything stale.
func (m *Model) completionTyped(c *lspCompletion, text string) (tea.Cmd, bool) {
	doc := m.ed.Active()
	if doc == nil || c == nil || docPath(doc) != c.path {
		return nil, false
	}
	r, ok := singleRune(text)
	if !ok {
		return nil, false
	}
	wordChars := m.docLanguageSettings(doc).WordCharacters
	if !editor.IsWordRune(r, wordChars) {
		return nil, false
	}
	// Apply the insertion exactly as the normal typing path does, so multi-cursor
	// edits and undo behave the same; the character is in the document afterwards.
	insert := m.dispatch(Command{Action: "insert_char", Args: map[string]any{"text": text}})
	at := doc.Cursor()
	line := doc.Line(at.Line)
	prefix := editor.CompletionPrefix(line, at.Col, wordChars)
	if prefix == "" {
		m.closeCompletion(c)
		return insert, true
	}
	wordStart := buffer.Pos{Line: at.Line, Col: max(at.Col-len([]rune(prefix)), 0)}
	filtered := filterCompletionItems(c.items, prefix, wordStart, at)
	if len(filtered) == 0 {
		// Nothing left to show: dismiss and ask once, which may fill the list again
		// (local words first, then the server) for the new prefix.
		m.closeCompletion(c)
		return tea.Batch(insert, m.completionAsked()), true
	}
	// Replace the open list's rows with the filtered ones. The list is rebuilt the
	// same way a server merge rebuilds it, so accept and its edit guards are shared.
	m.closeCompletion(c)
	reopen := m.lspReply(lspReplyMsg{ask: lspAskCompletion, path: c.path, at: at, items: filtered})
	return tea.Batch(insert, reopen), true
}

// filterCompletionItems keeps the items whose label or insert text starts with
// prefix (case-insensitively, as Fresh filters label/filterText), and gives an
// item without a server edit range one that covers the text typed since the list
// was asked, so accepting it replaces that text instead of duplicating the prefix.
func filterCompletionItems(items []CompletionItem, prefix string, start, at buffer.Pos) []CompletionItem {
	lowerPrefix := strings.ToLower(prefix)
	out := make([]CompletionItem, 0, len(items))
	for _, item := range items {
		label := strings.ToLower(item.Label)
		insert := strings.ToLower(item.Insert)
		if !strings.HasPrefix(label, lowerPrefix) && (insert == "" || !strings.HasPrefix(insert, lowerPrefix)) {
			continue
		}
		if item.Range == nil {
			item.Range = &LSPRange{Start: start, End: at}
		}
		out = append(out, item)
	}
	return out
}
