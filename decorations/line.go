package decorations

import "slices"

// The per-line query is answered from a window of windowLines lines that is built, by one
// pass over the items that reach it, on the first query of one of its lines and kept until
// the document's decorations change. A frame asks for consecutive lines, so it costs one
// pass per window and a lookup per line, whatever the size of the document or the number
// of items outside the window.
const (
	windowLines = 64
	// maxWindows bounds the windows kept for a document (a document shown in several
	// panes, or scrolled).
	maxWindows = 8
)

// window is the LineDecor of the lines [first, first+windowLines).
type window struct {
	first int
	lines [windowLines]LineDecor
	// top holds, while the window is built, the entries of each line that are painted on
	// top (at most MaxPerLine of each kind); finish turns them into the slices of lines.
	top [windowLines]lineTop
}

// lineTop is the bounded set of the entries of one line, kept while the window is built.
// The gutter and scroll marks keep the sequence number of the one that holds the slot.
type lineTop struct {
	rows           topK[RowStyle]
	spans          topK[Span]
	eol            topK[VirtualText]
	replaces       topK[Replacement]
	gutter, scroll uint64
	// tier-1c kinds
	above, below topK[VirtualRow]
	conceals     topK[ConcealSpan]
	softBreaks   topK[SoftBreakMark]
	cursorLines  topK[CursorLineOverlay]
}

// topK keeps the MaxPerLine entries that rank highest, by priority and then by sequence
// number, as a min-heap whose root is the lowest of them. A full set takes a new entry in
// O(log MaxPerLine) when it ranks above the root and refuses it in O(1) otherwise, so
// building a window costs time in the number of items that reach it and not more, whatever
// their priorities.
type topK[T any] struct {
	e []topEntry[T]
}

type topEntry[T any] struct {
	v    T
	prio int
	seq  uint64
}

func (a topEntry[T]) less(b topEntry[T]) bool {
	return a.prio < b.prio || (a.prio == b.prio && a.seq < b.seq)
}

// wants reports whether an entry of the priority and sequence number would be kept, so a
// caller can skip building the value of one that is not.
func (k *topK[T]) wants(prio int, seq uint64) bool {
	if len(k.e) < MaxPerLine {
		return true
	}
	return k.e[0].less(topEntry[T]{prio: prio, seq: seq})
}

// push adds the entry, dropping the lowest one when the set is full.
func (k *topK[T]) push(v T, prio int, seq uint64) {
	en := topEntry[T]{v: v, prio: prio, seq: seq}
	if len(k.e) < MaxPerLine {
		k.e = append(k.e, en)
		for i := len(k.e) - 1; i > 0; { // sift up
			p := (i - 1) / 2
			if !k.e[i].less(k.e[p]) {
				break
			}
			k.e[i], k.e[p] = k.e[p], k.e[i]
			i = p
		}
		return
	}
	if !k.e[0].less(en) {
		return
	}
	k.e[0] = en
	for i := 0; ; { // sift down
		l, r, m := 2*i+1, 2*i+2, i
		if l < len(k.e) && k.e[l].less(k.e[m]) {
			m = l
		}
		if r < len(k.e) && k.e[r].less(k.e[m]) {
			m = r
		}
		if m == i {
			return
		}
		k.e[i], k.e[m] = k.e[m], k.e[i]
		i = m
	}
}

// sorted returns the values in painting order: lowest priority first, and of equal
// priority the earlier one first.
func (k *topK[T]) sorted() []T {
	if len(k.e) == 0 {
		return nil
	}
	slices.SortFunc(k.e, func(a, b topEntry[T]) int {
		switch {
		case a.less(b):
			return -1
		case b.less(a):
			return 1
		}
		return 0
	})
	out := make([]T, len(k.e))
	for i, en := range k.e {
		out[i] = en.v
	}
	return out
}

// Line returns the decorations of the line of the document. The result is valid until the
// store changes.
func (s *Store) Line(doc uint64, line int) LineDecor {
	s.flush(doc)
	d := s.docs[doc]
	if d == nil || line < 0 {
		return LineDecor{}
	}
	first := line - line%windowLines
	w := d.windows[first]
	if w == nil {
		w = d.build(first)
		if d.windows == nil || len(d.windows) >= maxWindows {
			d.windows = make(map[int]*window, maxWindows)
		}
		d.windows[first] = w
	}
	return w.lines[line-first]
}

// build collects the items that reach the lines [first, first+windowLines).
func (d *docDecor) build(first int) *window {
	w := &window{first: first}
	end := first + windowLines
	for _, g := range d.groups {
		g.sort()
		lo, _ := slices.BinarySearchFunc(g.items, first-g.maxSpan, func(e entry, line int) int {
			return e.FromLine - line
		})
		for i := lo; i < len(g.items); i++ {
			e := &g.items[i]
			if e.FromLine >= end {
				break
			}
			if e.lastLine() >= first {
				w.add(e)
			}
		}
	}
	w.finish()
	return w
}

// add puts the entry on each line of the window that it reaches.
func (w *window) add(e *entry) {
	at := func(line int) (int, bool) {
		i := line - w.first
		return i, i >= 0 && i < windowLines
	}
	switch e.Kind {
	case Highlight:
		last := e.ToLine
		if e.ToCol == 0 {
			last-- // the range ends at the start of a line: that line is not in it
		}
		for line := max(e.FromLine, w.first); line <= last && line < w.first+windowLines; line++ {
			i, _ := at(line)
			if !w.top[i].spans.wants(e.Priority, e.seq) {
				continue
			}
			sp := Span{Start: 0, End: -1, Style: e.Style, Priority: e.Priority}
			if line == e.FromLine {
				sp.Start = e.FromCol
			}
			if line == e.ToLine {
				sp.End = e.ToCol
			}
			w.top[i].spans.push(sp, e.Priority, e.seq)
		}
	case Line:
		if i, ok := at(e.FromLine); ok && w.top[i].rows.wants(e.Priority, e.seq) {
			w.top[i].rows.push(RowStyle{Style: e.Style, Priority: e.Priority}, e.Priority, e.seq)
		}
	case EOL:
		if i, ok := at(e.FromLine); ok && w.top[i].eol.wants(e.Priority, e.seq) {
			w.top[i].eol.push(VirtualText{Text: e.Text, Style: e.Style, Priority: e.Priority}, e.Priority, e.seq)
		}
	case Gutter:
		if i, ok := at(e.FromLine); ok {
			if g := w.lines[i].Gutter; g == nil || beats(e, g.Priority, w.top[i].gutter) {
				w.lines[i].Gutter = &Mark{Text: e.Text, Style: e.Style, Priority: e.Priority}
				w.top[i].gutter = e.seq
			}
		}
	case Scroll:
		last := e.FromLine
		if e.ToLine != e.FromLine || e.ToCol != e.FromCol {
			last = e.ToLine
			if e.ToCol == 0 {
				last--
			}
		}
		for line := max(e.FromLine, w.first); line <= last && line < w.first+windowLines; line++ {
			i, _ := at(line)
			if m := w.lines[i].Scroll; m == nil || beats(e, m.Priority, w.top[i].scroll) {
				w.lines[i].Scroll = &ScrollMark{Color: e.Color, Priority: e.Priority}
				w.top[i].scroll = e.seq
			}
		}
	case Replace:
		if i, ok := at(e.FromLine); ok && w.top[i].replaces.wants(e.Priority, e.seq) {
			w.top[i].replaces.push(Replacement{Col: e.FromCol, Text: e.Text, Style: e.Style, Priority: e.Priority}, e.Priority, e.seq)
		}
	case VirtualLineAbove, VirtualLineBelow:
		i, ok := at(e.FromLine)
		if !ok {
			return
		}
		row := VirtualRow{
			Text: e.Text, Style: e.Style, GutterText: e.GutterText,
			GutterStyle: e.GutterStyle, Priority: e.Priority, ComposeOnly: e.ComposeOnly,
			Overlays: append([]TextOverlay(nil), e.TextOverlays[:min(e.TextOverlayCount, maxTextOverlays)]...),
		}
		target := &w.top[i].above
		if e.Kind == VirtualLineBelow {
			target = &w.top[i].below
		}
		if target.wants(e.Priority, e.seq) {
			target.push(row, e.Priority, e.seq)
		}
	case Conceal:
		for line := max(e.FromLine, w.first); line <= min(e.ToLine, w.first+windowLines-1) && line <= e.ToLine; line++ {
			i, _ := at(line)
			if !w.top[i].conceals.wants(e.Priority, e.seq) {
				continue
			}
			sp := ConcealSpan{
				Text: e.Text, Style: e.Style, Priority: e.Priority, Activation: e.Activation,
				ComposeOnly: e.ComposeOnly,
			}
			sp.Start, sp.End = 0, -1
			if line == e.FromLine {
				sp.Start = e.FromCol
			}
			if line == e.ToLine {
				sp.End = e.ToCol
			}
			sp.Scope = [2]int{e.ScopeStart, e.ScopeEnd}
			w.top[i].conceals.push(sp, e.Priority, e.seq)
		}
	case SoftBreak:
		if i, ok := at(e.FromLine); ok && w.top[i].softBreaks.wants(e.Priority, e.seq) {
			w.top[i].softBreaks.push(SoftBreakMark{
				Col: e.FromCol, Indent: e.Indent, PrefixText: e.PrefixText, PrefixStyle: e.PrefixStyle,
				Priority: e.Priority, Activation: e.Activation, Scope: [2]int{e.ScopeStart, e.ScopeEnd},
				ComposeOnly: e.ComposeOnly,
			}, e.Priority, e.seq)
		}
	case CursorLine:
		if i, ok := at(e.FromLine); ok && w.top[i].cursorLines.wants(e.Priority, e.seq) {
			w.top[i].cursorLines.push(CursorLineOverlay{Style: e.Style, Priority: e.Priority}, e.Priority, e.seq)
		}
	}
}

// beats reports whether e takes a slot held by an item of priority prio added as seq: the
// higher priority wins, and of equal priority the later one.
func beats(e *entry, prio int, seq uint64) bool {
	return e.Priority > prio || (e.Priority == prio && e.seq > seq)
}

// finish puts every slice of the window in painting order.
func (w *window) finish() {
	for i := range w.lines {
		l, t := &w.lines[i], &w.top[i]
		l.Rows, l.Spans, l.EOL, l.Replaces = t.rows.sorted(), t.spans.sorted(), t.eol.sorted(), t.replaces.sorted()
		l.Above, l.Below = t.above.sorted(), t.below.sorted()
		l.Conceals, l.SoftBreaks = t.conceals.sorted(), t.softBreaks.sorted()
		l.CursorLine = t.cursorLines.sorted()
		*t = lineTop{}
	}
}
