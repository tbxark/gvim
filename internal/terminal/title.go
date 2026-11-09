package terminal

// The terminal title a child sets with OSC 0 (icon+window title) or OSC 2 (window
// title). Fresh surfaces the same sequence as Event::Title and uses it for the
// terminal tab's name; gvim keeps it on the session, persists it with the terminal
// record and uses the recorded value as a tab-label fallback. OSC 1 only sets the
// icon name in the emulator, so it does not change the title here. A reset (an
// empty payload) clears the title. The value is cleaned (control characters
// stripped) and bounded so a hostile child cannot grow the record or paint escape
// sequences into the tab strip.

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxTitleRunes bounds one terminal title (and one saved title).
const MaxTitleRunes = 256

// setTitle stores a title from the emulator's Title callback.
func (s *Session) setTitle(title string) {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	s.title = CleanTitle(title)
}

// Title returns the terminal title the child last set with OSC 0/2, cleaned and
// bounded; "" when it never set one or reset it.
func (s *Session) Title() string {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	return s.title
}

// CleanTitle strips control characters and bounds a terminal title on a rune
// boundary. It is the one place a title becomes safe to keep, paint, persist or
// restore (restore.LoadTerminal and the dock use it too).
func CleanTitle(title string) string {
	// Drop invalid UTF-8 bytes rather than letting them become replacement runes.
	title = strings.ToValidUTF8(title, "")
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, title)
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) <= MaxTitleRunes {
		return title
	}
	runes := []rune(title)
	return strings.TrimSpace(string(runes[:MaxTitleRunes]))
}
