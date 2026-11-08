package editorpane

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/display"
)

// Options are the editor.* display options that the pane draws. The zero value is
// the plain pane: no hanging indent, no relative numbers, no bracket highlight, the
// guides of Pane.IndentGuides at every level in "│", and the whitespace marks of
// Pane.VisibleWhitespace everywhere.
type Options struct {
	// RelativeLineNumbers numbers each line by its distance from the caret line,
	// which keeps its own number.
	RelativeLineNumbers bool
	// WrapIndent gives the continuation rows of a wrapped line the leading blanks of
	// the line (the hanging indent). Wrap must be on.
	WrapIndent bool
	// WrapColumn is the text column where lines wrap when it is smaller than the
	// window. Zero wraps at the window edge.
	WrapColumn int
	// GuideActive draws only the innermost guide of the block around the caret,
	// where Pane.IndentGuides alone draws every level.
	GuideActive bool
	// GuideGlyph is the guide rune. Zero reads as "│". A rune that is not one cell
	// wide reads as "▏".
	GuideGlyph rune
	// RainbowGuides colors each guide by its indent level.
	RainbowGuides bool
	// Whitespace says which whitespace marks to draw. Nil means the marks of
	// Pane.VisibleWhitespace: every space and tab, and marks inside a selection.
	Whitespace *Whitespace
	// HighlightBrackets marks the bracket at the caret and its partner.
	HighlightBrackets bool
	// RainbowBrackets colors every bracket by its nesting depth, and then the pair
	// at the caret is marked by bold and underline. It needs HighlightBrackets.
	RainbowBrackets bool
}

// Whitespace says which whitespace marks the pane draws: a middle dot for a space,
// an arrow for a tab, and the end-of-line marks. A mark is drawn by the position of
// the blank in its line: leading before the first other character, trailing after
// the last, inner between. A line of blanks only is both leading and trailing.
type Whitespace struct {
	SpacesLeading, SpacesInner, SpacesTrailing bool
	TabsLeading, TabsInner, TabsTrailing       bool
	Newlines                                   bool // "↵" at the end of every line but the last
	CarriageReturns                            bool // "␍" before it, when the document has CRLF breaks
	// InSelection draws the space and tab marks inside a selection whatever the
	// flags above say.
	InSelection bool
}

// LineEnder is implemented by a Document that knows its line ending, as ui.Document
// does. The pane draws the "␍" mark only for "CRLF".
type LineEnder interface {
	LineEnding() string
}

// guideMode is how the indent guides are drawn.
type guideMode int

const (
	guidesOff guideMode = iota
	guidesAll
	guidesActive
)

// minSelectionMatch and maxSelectionMatch bound the selected text whose other
// occurrences are marked, in bytes. A shorter text would mark most of the screen.
const (
	minSelectionMatch = 2
	maxSelectionMatch = 512
)

// frameFX is the per-call state of the options. It is shared by the copies of the
// frame, so the scans that must see the lines in order keep their place.
type frameFX struct {
	ws      Whitespace
	crlf    bool
	hang    bool
	guide   guideMode
	glyph   rune
	rainbow bool

	// The guide scanner is created when the first line is painted, primed from the
	// lines above it. activeCol is the guide column of the block around the caret,
	// drawn on the lines activeFrom to activeTo.
	scan                 *display.GuideScanner
	activeCol            int
	activeFrom, activeTo int
	hasActive            bool
	brackets             bracketScan

	// occSel is the selected text whose other occurrences are marked. The selection
	// itself is the rune columns [occFrom, occTo) of line occLine.
	occSel                  string
	occLine, occFrom, occTo int

	// wins are the windows of the long lines laid out in this frame, by line.
	wins map[int]window
	// long says which lines are painted through a window, once asked, by line.
	long map[int]bool
	// tall says which lines are wrapped in segments, once asked, by line. segs are the
	// segments laid out in this frame. See longwrap.go.
	tall map[int]bool
	segs map[segKey]*segment
	// plain are the lines laid out without paint attributes in this frame, by line.
	plain map[int]line
	// caretRow is the display row of the caret in its own line, once known.
	caretRow   int
	caretRowOK bool
}

// initFX fills the option state of the frame. It runs once the selections and the
// caret are known.
func (f *frame) initFX(p Pane) {
	o := p.Opts
	fx := &frameFX{hang: o.WrapIndent && p.wrapOn(), glyph: o.GuideGlyph, rainbow: o.RainbowGuides}
	f.fx = fx
	f.opts = o
	if o.WrapColumn > 0 && f.wrapW > o.WrapColumn {
		f.wrapW = o.WrapColumn
	}
	fx.ws = p.whitespace()
	if f.noTabArrows {
		fx.ws.TabsLeading, fx.ws.TabsInner, fx.ws.TabsTrailing = false, false, false
	}
	if le, ok := p.Doc.(LineEnder); ok {
		fx.crlf = le.LineEnding() == "CRLF"
	}
	if fx.glyph == 0 {
		fx.glyph = '│'
	} else if ansi.StringWidth(string(fx.glyph)) != 1 {
		fx.glyph = '▏'
	}
	switch {
	case !p.IndentGuides:
	case o.GuideActive:
		fx.guide = guidesActive
	default:
		fx.guide = guidesAll
	}
	f.setOccurrence(p)
}

// whitespace is the whitespace marks of the pane: Opts.Whitespace, or every space
// and tab when only VisibleWhitespace is set.
func (p Pane) whitespace() Whitespace {
	if p.Opts.Whitespace != nil {
		return *p.Opts.Whitespace
	}
	w := Whitespace{InSelection: true}
	if p.VisibleWhitespace {
		w.SpacesLeading, w.SpacesInner, w.SpacesTrailing = true, true, true
		w.TabsLeading, w.TabsInner, w.TabsTrailing = true, true, true
	}
	return w
}

// setOccurrence picks what the occurrence highlight marks. With no selection it is
// the word at the caret. A selection on one line takes over, and then the target is
// its text, matched anywhere in a line. Any other selection marks nothing.
func (f *frame) setOccurrence(p Pane) {
	if !p.Occurrences || f.caret.Line >= f.lines {
		return
	}
	a, h, ok := p.Doc.Selection()
	if !ok || a == h {
		runes, off := f.nearRunes(f.caret.Line, f.caret.Col)
		f.occWord = wordAt(runes, f.caret.Col-off)
		return
	}
	if a.Line != h.Line || a.Line >= f.lines {
		return
	}
	from, to := min(a.Col, h.Col), max(a.Col, h.Col)
	if from < 0 || to-from > maxSelectionMatch {
		return
	}
	runes, off := f.nearRunes(a.Line, from)
	if to-off > len(runes) {
		return
	}
	text := string(runes[from-off : to-off])
	if !matchableSelection(text) {
		return
	}
	f.fx.occSel, f.fx.occLine, f.fx.occFrom, f.fx.occTo = text, a.Line, from, to
}

// matchableSelection reports whether the other occurrences of the selected text are
// marked: it is not blank and its size is within the bounds.
func matchableSelection(text string) bool {
	return len(text) >= minSelectionMatch && len(text) <= maxSelectionMatch &&
		strings.TrimSpace(text) != "" && !strings.ContainsAny(text, "\r\n")
}
