// Package decor is the store of the decorations that plugins put on documents: virtual text,
// styles, gutter marks and scrollbar marks that are drawn over a document and are not part
// of it. It is pure data: it imports the standard library only and has no notion of colors
// as the terminal knows them (a Color is a theme key or a literal RGB value, resolved by the
// painter) or of the plugin API.
//
// Items are kept per document, owner (a plugin) and namespace. Their positions are anchors:
// the editor reports every change of a document to Apply, which moves the anchors the way
// the text moves, so a decoration stays on the text it was put on. All columns are UTF-8
// byte columns of the line, as the plugin API speaks them; the painter converts to rune
// columns for the one line it paints.
//
// The store answers per line (Line) and never needs the document text, so a large document
// costs no more than a small one. A Store is not safe for concurrent use: it lives on the
// goroutine that edits and paints.
package decor

import (
	"errors"
	"slices"
	"sort"
)

// Kind is the kind of an item.
type Kind uint8

// The kinds of item.
const (
	// Highlight styles the cells of the range [From, To).
	Highlight Kind = iota
	// Line styles the whole rows of line FromLine.
	Line
	// EOL is virtual Text drawn after the last cell of line FromLine.
	EOL
	// Gutter is the one-cell Text that replaces the change bar of line FromLine.
	Gutter
	// Scroll marks a line, or the lines of a range, in the last column with Color. It is a
	// range when To is after From, else the line FromLine.
	Scroll
	// Replace draws Text over the cell at (FromLine, FromCol).
	Replace
)

// Color is a theme key ("section.key"), a semantic name or a literal RGB value, as the
// plugin gave it. It is resolved to a terminal color when it is painted.
type Color struct {
	Key     string
	R, G, B uint8
	Set     bool // R, G, B are valid
}

// IsZero reports whether the color is unset.
func (c Color) IsZero() bool { return c.Key == "" && !c.Set }

// Style is the look of an item.
type Style struct {
	FG, BG                                           Color
	Bold, Italic, Underline, Strike, Reverse, Extend bool // Extend: the background runs to the end of the row
}

// Item is one decoration. Positions are byte columns. A kind that sits on a line (Line,
// EOL, Gutter, a Scroll of one line) is anchored to the start of line FromLine and ignores
// the other position fields.
type Item struct {
	Kind                             Kind
	FromLine, FromCol, ToLine, ToCol int
	Text                             string
	Style                            Style
	Color                            Color
	Priority                         int
}

// Edit is one change of a document, in byte columns: the text from (FromLine, FromCol) to
// (ToLine, ToCol) is replaced by Text. The coordinates are those of the document just
// before this edit; a sequence of edits is applied first to last.
type Edit struct {
	FromLine, FromCol, ToLine, ToCol int
	Text                             string
}

// Limits of one owner. A plugin that reaches one gets ErrFull.
const (
	MaxPerNamespace = 20_000 // items of one (owner, namespace, document)
	MaxPerOwner     = 50_000 // items of one owner over all documents
	// MaxPerLine bounds the items of one kind that Line returns for a line, so a plugin
	// that piles thousands of items on one line cannot make a frame slow. The ones with the
	// highest priority are kept.
	MaxPerLine = 256
)

// ErrFull is returned when an owner has too many items.
var ErrFull = errors.New("decor: too many decorations")

// Span is a styled run of one line: byte columns [Start, End). End is -1 when the run goes
// on to the end of the line and past it (the line break), for a range that continues on the
// next line.
type Span struct {
	Start, End int
	Style      Style
	Priority   int
}

// RowStyle is the style of a whole row.
type RowStyle struct {
	Style    Style
	Priority int
}

// VirtualText is text drawn after the end of a line.
type VirtualText struct {
	Text     string
	Style    Style
	Priority int
}

// Mark is the cell that replaces the change bar of a line.
type Mark struct {
	Text     string
	Style    Style
	Priority int
}

// ScrollMark is the mark of a line in the last column.
type ScrollMark struct {
	Color    Color
	Priority int
}

// Replacement is text that is drawn over the cell at byte column Col.
type Replacement struct {
	Col      int
	Text     string
	Style    Style
	Priority int
}

// LineDecor is everything the store holds for one line. Every slice is in painting order:
// by priority, lowest first, and items of equal priority in the order they were added, so
// the later one is drawn over the earlier. The slices are shared with the store: callers
// must not change them, and must not keep them across a change of the store.
type LineDecor struct {
	Rows     []RowStyle
	Spans    []Span
	EOL      []VirtualText
	Gutter   *Mark
	Scroll   *ScrollMark
	Replaces []Replacement
}

// IsZero reports whether the line has no decoration.
func (d LineDecor) IsZero() bool {
	return len(d.Rows) == 0 && len(d.Spans) == 0 && len(d.EOL) == 0 && d.Gutter == nil && d.Scroll == nil && len(d.Replaces) == 0
}

// Store holds the decorations of all documents.
type Store struct {
	docs     map[uint64]*docDecor
	perOwner map[string]int
	seq      uint64 // order of addition, for ties of priority
	rev      uint64
	queued   int // documents with queued edits (Queue)
}

// New returns an empty store.
func New() *Store {
	return &Store{docs: map[uint64]*docDecor{}, perOwner: map[string]int{}}
}

// docDecor is the decorations of one document.
type docDecor struct {
	groups  []*group
	windows map[int]*window // built on demand, dropped with every change of the document
	pend    []shape         // edits queued by Queue, not yet applied to the items
}

// group is the items of one (owner, namespace) on one document, sorted by FromLine.
type group struct {
	owner, ns string
	items     []entry
	sorted    bool
	maxSpan   int // an upper bound of ToLine-FromLine over the items
	nextID    int
	// dead counts the items that Remove marked (id 0) and that compact has not yet taken
	// out, so removing many items one by one does not shift the slice each time.
	dead int
	// index maps the id of a live item to its place in items. It is built by the first
	// Remove and dropped whenever the items move.
	index map[int]int
}

// size is the number of live items.
func (g *group) size() int { return len(g.items) - g.dead }

// compact takes the items that Remove marked out of the slice.
func (g *group) compact() {
	if g.dead == 0 {
		return
	}
	g.items = slices.DeleteFunc(g.items, func(e entry) bool { return e.id == 0 })
	g.dead, g.index = 0, nil
}

type entry struct {
	Item
	id  int
	seq uint64
}

// lastLine is the last line the item can show on.
func (e *entry) lastLine() int {
	if e.Kind == Highlight || e.Kind == Scroll {
		return e.ToLine
	}
	return e.FromLine
}

// Rev counts the changes of the store. A cache of anything derived from it is valid while
// Rev is the same.
func (s *Store) Rev() uint64 { return s.rev }

// Empty reports whether the store holds no item at all.
func (s *Store) Empty() bool {
	s.flush(0)
	return len(s.docs) == 0
}

// Count returns the number of items on a document.
func (s *Store) Count(doc uint64) int {
	s.flush(doc)
	d := s.docs[doc]
	if d == nil {
		return 0
	}
	n := 0
	for _, g := range d.groups {
		n += g.size()
	}
	return n
}

// Items returns a copy of the items of (owner, ns) on the document, in the order the store
// keeps them (by start line). It is for tests and diagnostics: painting uses Line.
func (s *Store) Items(doc uint64, owner, ns string) []Item {
	s.flush(doc)
	d := s.docs[doc]
	if d == nil {
		return nil
	}
	g := d.group(owner, ns)
	if g == nil {
		return nil
	}
	g.sort()
	out := make([]Item, len(g.items))
	for i, e := range g.items {
		out[i] = e.Item
	}
	return out
}

// OwnerCount returns the number of items of an owner over all documents.
func (s *Store) OwnerCount(owner string) int {
	s.flush(0)
	return s.perOwner[owner]
}

// Docs returns the documents that have decorations, in no particular order.
func (s *Store) Docs() []uint64 {
	s.flush(0)
	return s.docIDs()
}

func (s *Store) docIDs() []uint64 {
	out := make([]uint64, 0, len(s.docs))
	for id := range s.docs {
		out = append(out, id)
	}
	return out
}

// normalize puts an item in canonical form and reports whether it can show at all. A
// negative position is the start of the document, a reversed range is turned round, and
// an empty range shows nothing.
func normalize(it Item) (Item, bool) {
	it.FromLine, it.FromCol = max(it.FromLine, 0), max(it.FromCol, 0)
	it.ToLine, it.ToCol = max(it.ToLine, 0), max(it.ToCol, 0)
	switch it.Kind {
	case Highlight:
		if it.ToLine < it.FromLine || (it.ToLine == it.FromLine && it.ToCol < it.FromCol) {
			it.FromLine, it.FromCol, it.ToLine, it.ToCol = it.ToLine, it.ToCol, it.FromLine, it.FromCol
		}
		if it.ToLine == it.FromLine && it.ToCol == it.FromCol {
			return it, false
		}
	case Scroll:
		// A range when To is after From, else the one line FromLine.
		if it.ToLine < it.FromLine || (it.ToLine == it.FromLine && it.ToCol <= it.FromCol) {
			it.FromCol, it.ToLine, it.ToCol = 0, it.FromLine, 0
		}
	case Replace:
		it.ToLine, it.ToCol = it.FromLine, it.FromCol
	default: // a line item
		it.FromCol, it.ToLine, it.ToCol = 0, it.FromLine, 0
	}
	return it, true
}

func (g *group) add(it Item, seq uint64) int {
	g.nextID++
	g.items = append(g.items, entry{Item: it, id: g.nextID, seq: seq})
	if g.index != nil {
		g.index[g.nextID] = len(g.items) - 1
	}
	if n := len(g.items); n > 1 && g.items[n-2].FromLine > it.FromLine {
		g.sorted = false
	}
	if it.Kind == Highlight || it.Kind == Scroll {
		g.maxSpan = max(g.maxSpan, it.ToLine-it.FromLine)
	}
	return g.nextID
}

func (g *group) sort() {
	g.compact()
	if g.sorted {
		return
	}
	sort.SliceStable(g.items, func(i, j int) bool { return g.items[i].FromLine < g.items[j].FromLine })
	g.sorted, g.index = true, nil
}

func (d *docDecor) group(owner, ns string) *group {
	for _, g := range d.groups {
		if g.owner == owner && g.ns == ns {
			return g
		}
	}
	return nil
}

func (s *Store) doc(doc uint64, create bool) *docDecor {
	d := s.docs[doc]
	if d == nil && create {
		d = &docDecor{}
		s.docs[doc] = d
	}
	return d
}

// changed records a change of the store and drops what is derived from the document.
func (s *Store) changed(d *docDecor) {
	s.rev++
	if d != nil {
		d.windows = nil
	}
}

// tidy removes the empty groups and documents that a change left behind.
func (s *Store) tidy(doc uint64) {
	d := s.docs[doc]
	if d == nil {
		return
	}
	d.groups = slices.DeleteFunc(d.groups, func(g *group) bool { return g.size() == 0 })
	if len(d.groups) == 0 {
		delete(s.docs, doc)
	}
}

// Set replaces everything that (owner, ns) has on the document by items. Items that show
// nothing (an empty range) are left out. It returns ErrFull, and changes nothing, when the
// namespace would hold more than MaxPerNamespace items or the owner more than MaxPerOwner.
func (s *Store) Set(doc uint64, owner, ns string, items []Item) error {
	s.flush(doc)
	norm := make([]Item, 0, len(items))
	for _, it := range items {
		if n, ok := normalize(it); ok {
			norm = append(norm, n)
		}
	}
	d := s.doc(doc, len(norm) > 0)
	var g *group
	old := 0
	if d != nil {
		if g = d.group(owner, ns); g != nil {
			old = g.size()
		}
	}
	if len(norm) > MaxPerNamespace || s.perOwner[owner]-old+len(norm) > MaxPerOwner {
		s.tidy(doc)
		return ErrFull
	}
	if d == nil {
		return nil
	}
	if g == nil {
		g = &group{owner: owner, ns: ns}
		d.groups = append(d.groups, g)
	}
	g.items, g.maxSpan, g.sorted, g.dead, g.index = g.items[:0], 0, true, 0, nil
	for _, it := range norm {
		s.seq++
		g.add(it, s.seq)
	}
	s.perOwner[owner] += len(norm) - old
	s.changed(d)
	s.tidy(doc)
	return nil
}

// Add adds one item to (owner, ns) on the document and returns its id, which Remove takes.
// An item that shows nothing is not added and has id 0. It returns ErrFull as Set does.
func (s *Store) Add(doc uint64, owner, ns string, it Item) (int, error) {
	s.flush(doc)
	it, ok := normalize(it)
	if !ok {
		return 0, nil
	}
	d := s.doc(doc, true)
	g := d.group(owner, ns)
	if g == nil {
		g = &group{owner: owner, ns: ns, sorted: true}
		d.groups = append(d.groups, g)
	}
	if g.size() >= MaxPerNamespace || s.perOwner[owner] >= MaxPerOwner {
		s.tidy(doc)
		return 0, ErrFull
	}
	s.seq++
	id := g.add(it, s.seq)
	s.perOwner[owner]++
	s.changed(d)
	return id, nil
}

// Remove removes the item with the id from (owner, ns) on the document, and reports
// whether there was one.
func (s *Store) Remove(doc uint64, owner, ns string, id int) bool {
	s.flush(doc)
	d := s.docs[doc]
	if d == nil {
		return false
	}
	g := d.group(owner, ns)
	if g == nil {
		return false
	}
	if g.index == nil {
		g.index = make(map[int]int, len(g.items))
		for i, e := range g.items {
			if e.id != 0 {
				g.index[e.id] = i
			}
		}
	}
	i, ok := g.index[id]
	if !ok {
		return false
	}
	delete(g.index, id)
	g.items[i].id = 0
	g.dead++
	if g.dead > 32 && g.dead*2 > len(g.items) {
		g.compact()
	}
	s.perOwner[owner]--
	s.changed(d)
	s.tidy(doc)
	return true
}

// Clear removes everything (owner, ns) has on the document. Document 0 means every
// document.
func (s *Store) Clear(doc uint64, owner, ns string) {
	s.flush(doc)
	if doc != 0 {
		s.clearDoc(doc, owner, func(g *group) bool { return g.ns == ns })
		return
	}
	for id := range s.docs {
		s.clearDoc(id, owner, func(g *group) bool { return g.ns == ns })
	}
}

// DropOwner removes everything the owner has, on every document.
func (s *Store) DropOwner(owner string) {
	s.flush(0)
	for id := range s.docs {
		s.clearDoc(id, owner, func(*group) bool { return true })
	}
	delete(s.perOwner, owner)
}

// DropDoc removes everything on the document: it was closed.
func (s *Store) DropDoc(doc uint64) {
	s.flush(doc)
	d := s.docs[doc]
	if d == nil {
		return
	}
	for _, g := range d.groups {
		s.perOwner[g.owner] -= g.size()
	}
	delete(s.docs, doc)
	s.rev++
}

func (s *Store) clearDoc(doc uint64, owner string, match func(*group) bool) {
	d := s.docs[doc]
	if d == nil {
		return
	}
	hit := false
	for _, g := range d.groups {
		if g.owner == owner && match(g) {
			s.perOwner[owner] -= g.size()
			g.items, g.dead, g.index = nil, 0, nil
			hit = true
		}
	}
	if hit {
		s.changed(d)
		s.tidy(doc)
	}
}
