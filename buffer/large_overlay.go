package buffer

import (
	"errors"
	"fmt"
	"strings"
)

// pieceCount is the number of pieces in the subtree.
func (n *node) pieceCount() int {
	if n == nil {
		return 0
	}
	return n.pieces
}

// lineDirty reports whether document line i is a literal line of the overlay,
// that is, a line that was typed, pasted or rewritten.
func (s *bigStore) lineDirty(i int) bool {
	return i >= 0 && i < s.lines() && pieceKindAt(s.st.root, i) == kindLiteral
}

// empty reports whether the document has no text. Until the index is complete
// the lines still to come are unknown, so it is never empty.
func (s *bigStore) empty() bool {
	return s.g.complete() && s.docLines() == 1 && s.lineBytesLen(0) == 0
}

// OverlayPiece is one piece of the overlay of unsaved edits over a large
// buffer's source. A run piece stands for base lines [First, First+Count) of
// the source, unchanged. A literal piece holds the text of lines that were
// typed, pasted or rewritten.
type OverlayPiece struct {
	Run   bool `json:"run,omitzero"`
	First int  `json:"first,omitzero"`
	Count int  `json:"count,omitzero"`
	// Lines is the text of the literal lines, without terminators.
	Lines []string `json:"lines,omitempty"`
	// EOL has one character per literal line: '-' for the document's default
	// terminator, 'n' for LF and 'r' for CRLF. It is empty when every line has
	// the default.
	EOL string `json:"eol,omitempty"`
}

// OverlayState is the unsaved part of a large buffer, as a delta over its
// source: the document is the lines of Pieces in order, then the base lines
// from TailFirst on. Run pieces reference base lines before TailFirst only.
// It holds the edited text and no text of the source, so it is small however
// large the file is, and it does not depend on how much of the line index is
// built. See Buffer.Overlay and Buffer.RestoreOverlay.
type OverlayState struct {
	Pieces    []OverlayPiece `json:"pieces,omitempty"`
	TailFirst int            `json:"tailFirst,omitzero"`
}

// Empty reports whether the state has no edits: restoring it changes nothing.
func (o OverlayState) Empty() bool { return len(o.Pieces) == 0 && o.TailFirst == 0 }

// ErrOverlayInvalid is wrapped by the error RestoreOverlay returns for a state
// that cannot be an overlay of the buffer's source: ranges that are out of
// order or outside the source, or lines that cannot be stored.
var ErrOverlayInvalid = errors.New("buffer: overlay does not fit the file")

// errOverlayBusy is returned by RestoreOverlay for a buffer that already holds
// edits.
var errOverlayBusy = errors.New("buffer: overlay can only be restored onto an unedited buffer")

// The characters of OverlayPiece.EOL.
const (
	eolFlagDefault = '-'
	eolFlagLF      = 'n'
	eolFlagCRLF    = 'r'
)

// eolFlags writes the terminator flags of a literal piece as the EOL string of
// an OverlayPiece.
func eolFlags(p piece) string {
	if p.eol == nil {
		return ""
	}
	out := make([]byte, len(p.eol))
	for i, e := range p.eol {
		switch e {
		case eolLF:
			out[i] = eolFlagLF
		case eolCRLF:
			out[i] = eolFlagCRLF
		default:
			out[i] = eolFlagDefault
		}
	}
	return string(out)
}

// collectPieces appends the pieces of n, in document order, to out.
func collectPieces(n *node, out []OverlayPiece) []OverlayPiece {
	if n == nil {
		return out
	}
	out = collectPieces(n.l, out)
	if n.p.isRun() {
		out = append(out, OverlayPiece{Run: true, First: n.p.first, Count: n.p.count})
	} else {
		lines := make([]string, len(n.p.lit))
		for i, rs := range n.p.lit {
			lines[i] = string(rs)
		}
		out = append(out, OverlayPiece{Lines: lines, EOL: eolFlags(n.p)})
	}
	return collectPieces(n.r, out)
}

// Overlay returns the unsaved edits of a large buffer as a delta over its
// source. It is true for every large buffer, whether the line index is
// complete or not, and false for a memory buffer. The cost is the size of the
// edited lines, never the size of the file. Restore it with RestoreOverlay on
// a buffer over the same bytes.
func (b *Buffer) Overlay() (OverlayState, bool) {
	s := b.big
	if s == nil {
		return OverlayState{}, false
	}
	return OverlayState{Pieces: collectPieces(s.st.root, nil), TailFirst: s.st.tailFirst}, true
}

// OverlayLater is Overlay with the work moved into the function it returns. Taking it
// costs nothing, whatever the size of the edits: it keeps the immutable overlay state.
// The function builds the OverlayState of the buffer as it was when OverlayLater ran, and
// may run on another goroutine while the buffer is edited. It is false for a memory buffer.
func (b *Buffer) OverlayLater() (func() OverlayState, bool) {
	s := b.big
	if s == nil {
		return nil, false
	}
	root, tail := s.st.root, s.st.tailFirst
	return func() OverlayState {
		return OverlayState{Pieces: collectPieces(root, nil), TailFirst: tail}
	}, true
}

// RestoreOverlay puts the edits of o onto a buffer that has none, as one undo
// step, and bumps the version, so the buffer is modified. It is for a buffer
// freshly opened over the bytes that the overlay was taken from; the caller
// checks that identity.
//
// It returns ErrIndexPending, and changes nothing, while the line index has
// not yet reached the base lines the overlay refers to; the caller tries again
// after each ApplyIndex. It returns an error that wraps ErrOverlayInvalid for
// a state that cannot fit the source, and ErrOverlayFull when it would pass the
// buffer's cap on unsaved bytes. An empty state restores nothing and succeeds.
func (b *Buffer) RestoreOverlay(o OverlayState) error {
	s := b.big
	switch {
	case s == nil:
		return errNotLarge
	case s.closed:
		return errClosed
	case b.depth > 0:
		return fmt.Errorf("%w: inside Group", errOverlayBusy)
	case s.st.root != nil || s.st.tailFirst != 0:
		return errOverlayBusy
	}
	pieces, err := s.overlayPieces(o)
	if err != nil {
		return err
	}
	if o.Empty() {
		return nil
	}
	if base := s.g.baseLines(); base < o.TailFirst {
		if !s.g.complete() {
			return ErrIndexPending
		}
		return fmt.Errorf("%w: it covers %d lines of a file of %d", ErrOverlayInvalid, o.TailFirst, base)
	}
	ns := bigState{tailFirst: o.TailFirst}
	for _, p := range pieces {
		ns.root = s.tree.merge(ns.root, s.tree.leaf(p))
	}
	cost := overlayBytes(ns)
	if cost > s.maxOverlay {
		return ErrOverlayFull
	}
	old := s.st
	s.st = ns
	b.redo = nil
	b.seq++
	b.undo = append(b.undo, &group{
		before: b.ver, after: b.seq, sb: old, sa: ns, cost: cost,
		ops: []op{{kind: opInsert, big: true}},
	})
	b.open = false
	b.ver = b.seq
	b.trimHistory()
	b.notifyCoarse(false)
	return nil
}

// overlayPieces checks the pieces of o against the rules of a document state
// and builds them. It looks at nothing but o, so the answer does not depend on
// how much of the index exists.
func (s *bigStore) overlayPieces(o OverlayState) ([]piece, error) {
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrOverlayInvalid, fmt.Sprintf(format, args...))
	}
	if o.TailFirst < 0 {
		return nil, invalid("negative tail line")
	}
	out := make([]piece, 0, len(o.Pieces))
	next := 0 // the first base line a run may start at
	for i, p := range o.Pieces {
		if p.Run {
			if len(p.Lines) > 0 || p.EOL != "" {
				return nil, invalid("piece %d is a run with text", i)
			}
			if p.First < next || p.Count <= 0 || p.First > o.TailFirst-p.Count {
				return nil, invalid("piece %d covers base lines %d+%d", i, p.First, p.Count)
			}
			next = p.First + p.Count
			out = append(out, runPiece(p.First, p.Count))
			continue
		}
		if len(p.Lines) == 0 || p.First != 0 || p.Count != 0 {
			return nil, invalid("piece %d is not a run and has no lines", i)
		}
		if p.EOL != "" && len(p.EOL) != len(p.Lines) {
			return nil, invalid("piece %d has %d terminators for %d lines", i, len(p.EOL), len(p.Lines))
		}
		ll := make([]litLine, len(p.Lines))
		for k, text := range p.Lines {
			if strings.Contains(text, "\n") {
				return nil, invalid("piece %d line %d holds a line break", i, k)
			}
			ll[k].runes = []rune(text)
			if p.EOL == "" {
				continue
			}
			switch p.EOL[k] {
			case eolFlagDefault:
			case eolFlagLF:
				ll[k].eol = eolLF
			case eolFlagCRLF:
				ll[k].eol = eolCRLF
			default:
				return nil, invalid("piece %d has terminator %q", i, p.EOL[k])
			}
		}
		out = append(out, litPiece(ll))
	}
	return out, nil
}
