package tui

import "github.com/tbxark/gvim/buffer"

// versionedDoc is a Document that reports the version of its text, which changes
// with every edit, undo and redo. Without it the occurrence highlight cannot tell an
// edit from a caret move, and an edit does not switch it off.
type versionedDoc interface {
	Version() uint64
}

// occKey names the document an occTrack follows.
type occKey struct {
	tab  int
	name string
}

// occTrack applies Fresh's rule for the occurrence highlight: an edit drops it, and it
// stays off until the caret moves for another reason or a selection appears. Typing
// changes the word under the caret on every key, so a highlight that came back after
// each key would restyle every match for nothing.
type occTrack struct {
	key     occKey
	seen    bool
	version uint64
	off     bool       // an edit switched the highlight off
	at      buffer.Pos // the caret the edit left it off at
}

// occurrencesOff reports whether the highlight is off for doc until the caret moves.
// It is true only for the active document that was edited.
func (m *Model) occurrencesOff(doc Document) bool {
	return m.occ.off && doc != nil && m.occ.key == occKey{m.activeTab(), doc.Name()}
}

// trackOccurrences follows the active document after a message: it notes an edit, and
// ends the pause when the caret has moved. A tab that was just switched to is only
// taken in, so switching tabs is not mistaken for an edit.
func (m *Model) trackOccurrences() {
	doc := m.ed.Active()
	v, ok := doc.(versionedDoc)
	if doc == nil || !ok {
		m.occ = occTrack{}
		return
	}
	key := occKey{m.activeTab(), doc.Name()}
	ver := v.Version()
	if !m.occ.seen || m.occ.key != key {
		m.occ = occTrack{key: key, seen: true, version: ver}
		return
	}
	cur := doc.Cursor()
	a, h, sel := doc.Selection()
	switch {
	case ver != m.occ.version:
		m.occ.off, m.occ.at = true, cur
	case m.occ.off && (cur != m.occ.at || (sel && a != h)):
		m.occ.off = false
	}
	m.occ.version = ver
}
