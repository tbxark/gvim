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
	if !m.refilterCompletion(c, prefix, wordStart, at) {
		// Nothing left to show: dismiss and ask once, which may fill the list again
		// (local words first, then the server) for the new prefix.
		m.closeCompletion(c)
		return tea.Batch(insert, m.completionAsked()), true
	}
	return insert, true
}

// completionBackspace deletes one character behind the caret, as the normal key path
// would, and re-filters the open completion list by the shorter word prefix. Fresh
// does exactly that (handle_popup_backspace + refilter_completion_popup); a filter
// that empties dismisses the list without asking the server again. It reports false
// when the document moved on, so the caller falls back to the normal dismissal.
func (m *Model) completionBackspace(c *lspCompletion) (tea.Cmd, bool) {
	doc := m.ed.Active()
	if doc == nil || c == nil || docPath(doc) != c.path {
		return nil, false
	}
	del := m.dispatch(Command{Action: "delete_backward"})
	at := doc.Cursor()
	if at == c.at || at.Line != c.at.Line || at.Col > c.at.Col {
		// The edit did not leave the caret in the word the list was asked at (first
		// column, another line): the list is stale, dismiss as before.
		m.closeCompletion(c)
		return del, true
	}
	wordChars := m.docLanguageSettings(doc).WordCharacters
	prefix := editor.CompletionPrefix(doc.Line(at.Line), at.Col, wordChars)
	wordStart := buffer.Pos{Line: at.Line, Col: max(at.Col-len([]rune(prefix)), 0)}
	if !m.refilterCompletion(c, prefix, wordStart, at) {
		m.closeCompletion(c)
		return del, true
	}
	return del, true
}

// refilterCompletion rebuilds c's shown rows from the COMPLETE candidate pool
// (c.all), not from the rows on screen, so a candidate that a longer prefix hid
// comes back when Backspace shortens the word (Fresh always rebuilds from its stored
// completion_items). It keeps the highlighted candidate by its index in all, and
// captures the document version after the edit for the acceptance guard. It reports
// whether any row is left; the caller closes the list when none is.
func (m *Model) refilterCompletion(c *lspCompletion, prefix string, wordStart, at buffer.Pos) bool {
	shown, origins := filterCompletionItemsOrigins(c.all, prefix, wordStart, at)
	if len(shown) == 0 {
		return false
	}
	selected := 0
	if c.pop != nil {
		oldRow := c.pop.Selected()
		if oldRow >= 0 && oldRow < len(c.origins) {
			oldAll := c.origins[oldRow]
			for i, origin := range origins {
				if origin == oldAll {
					selected = i
					break
				}
			}
		}
	}
	m.closeCompletion(c)
	m.openCompletionList(c.path, at, c.all, shown, origins, editor.Version{}, false, selected)
	return true
}

// filterCompletionItemsOrigins keeps the items whose label or insert text starts
// with prefix (case-insensitively, as Fresh filters label/filterText), gives an item
// without a server edit range one that covers the text typed since the list was
// asked (so accepting replaces that text instead of duplicating the prefix) and
// returns the index every kept item had in items. A synthetic range is recomputed on
// every re-filter: after Backspace it must cover the shorter word, not text the user
// has already deleted. The list keeps the highlighted row on the same candidate by
// that former index: labels can repeat (auto-import candidates), so the row's index
// is the stable identity across a re-filter.
func filterCompletionItemsOrigins(items []CompletionItem, prefix string, start, at buffer.Pos) ([]CompletionItem, []int) {
	lowerPrefix := strings.ToLower(prefix)
	out := make([]CompletionItem, 0, len(items))
	origins := make([]int, 0, len(items))
	for i, item := range items {
		label := strings.ToLower(item.Label)
		insert := strings.ToLower(item.Insert)
		if !strings.HasPrefix(label, lowerPrefix) && (insert == "" || !strings.HasPrefix(insert, lowerPrefix)) {
			continue
		}
		if item.Range == nil || item.rangeSynth {
			item.Range = &LSPRange{Start: start, End: at}
			item.rangeSynth = true
		}
		out = append(out, item)
		origins = append(origins, i)
	}
	return out, origins
}
