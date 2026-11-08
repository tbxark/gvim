package buffer

import "unicode/utf8"

// Terminator flags of a document line (see AMENDMENTS C14). Base lines carry
// their real terminator; literal lines carry the flag of the line they
// replaced. eolDefault means "use the document's default EOL on save".
const (
	eolDefault uint8 = iota
	eolLF
	eolCRLF
)

// litLine is the unit of construction of a literal piece: the runes of a line
// without terminator and its terminator flag.
type litLine struct {
	runes []rune
	eol   uint8
}

// piece is a run of document lines. A run piece (lit == nil) stands for base
// lines [first, first+count) of the source. A literal piece holds lines that
// were typed, pasted or re-encoded; lit and nb always have the same length,
// and eol is nil (all eolDefault) or has that length too. Pieces are never
// empty and never modified after they are published in a node.
type piece struct {
	first, count int
	lit          [][]rune // immutable rune slices, may alias a larger backing array
	eol          []uint8  // terminator flag per literal line, or nil
	nb           []int    // UTF-8 byte length per literal line
}

// isRun reports whether p stands for base lines.
func (p piece) isRun() bool { return p.lit == nil }

// length is the number of document lines p covers.
func (p piece) length() int {
	if p.lit == nil {
		return p.count
	}
	return len(p.lit)
}

// eolAt is the terminator flag of literal line k of p.
func (p piece) eolAt(k int) uint8 {
	if p.eol == nil {
		return eolDefault
	}
	return p.eol[k]
}

// runesBytes is the length of rs once encoded as UTF-8; invalid runes encode
// as U+FFFD, as string(rs) does.
func runesBytes(rs []rune) int {
	n := 0
	for _, r := range rs {
		if l := utf8.RuneLen(r); l > 0 {
			n += l
		} else {
			n += utf8.RuneLen(utf8.RuneError)
		}
	}
	return n
}

func runPiece(first, count int) piece { return piece{first: first, count: count} }

// litPiece builds a literal piece from lines (non-empty). The rune slices are
// adopted, not copied.
func litPiece(lines []litLine) piece {
	p := piece{
		lit: make([][]rune, len(lines)),
		nb:  make([]int, len(lines)),
	}
	for i, l := range lines {
		p.lit[i] = l.runes
		p.nb[i] = runesBytes(l.runes)
		if l.eol != eolDefault {
			if p.eol == nil {
				p.eol = make([]uint8, len(lines))
			}
			p.eol[i] = l.eol
		}
	}
	return p
}

// splitPiece cuts p after k of its lines (0 < k < p.length()). Literal halves
// alias p's arrays with their capacity clipped, so appending to one never
// overwrites the other.
func splitPiece(p piece, k int) (a, b piece) {
	if p.lit == nil {
		return runPiece(p.first, k), runPiece(p.first+k, p.count-k)
	}
	n := len(p.lit)
	a = piece{lit: p.lit[:k:k], nb: p.nb[:k:k]}
	b = piece{lit: p.lit[k:n:n], nb: p.nb[k:n:n]}
	if p.eol != nil {
		a.eol, b.eol = p.eol[:k:k], p.eol[k:n:n]
	}
	return a, b
}

// node is an immutable treap node ordered by document position. Every node
// holds one piece; lines, litRunes and litBytes summarise the subtree.
type node struct {
	l, r     *node
	prio     uint32
	p        piece
	lines    int // lines in subtree
	litRunes int // runes in literal lines of the subtree
	litBytes int // UTF-8 bytes of literal lines of the subtree
	litLines int // literal lines in the subtree
	pieces   int // pieces in the subtree
}

// bigState is the immutable document state of a large buffer: the overlay
// tree plus the first base line not covered by it. The document is the
// in-order pieces of root followed by base lines [tailFirst, baseLines).
type bigState struct {
	root      *node
	tailFirst int
}

func (n *node) size() int {
	if n == nil {
		return 0
	}
	return n.lines
}

// treeOps builds nodes. It owns the priority sequence, so the shape of every
// tree is a function of the operation sequence only (reproducible in tests).
type treeOps struct{ seq uint64 }

func (t *treeOps) nextPrio() uint32 {
	t.seq++
	z := t.seq * 0x9E3779B97F4A7C15
	z = (z ^ z>>30) * 0xBF58476D1CE4E5B9
	z = (z ^ z>>27) * 0x94D049BB133111EB
	z ^= z >> 31
	return uint32(z >> 32)
}

// mk builds a node with the given children, priority and piece.
func mk(l, r *node, prio uint32, p piece) *node {
	n := &node{l: l, r: r, prio: prio, p: p, lines: p.length(), pieces: 1, litLines: len(p.lit)}
	n.lines += l.size() + r.size()
	for i, rs := range p.lit {
		n.litRunes += len(rs)
		n.litBytes += p.nb[i]
	}
	for _, c := range [...]*node{l, r} {
		if c != nil {
			n.litRunes += c.litRunes
			n.litBytes += c.litBytes
			n.litLines += c.litLines
			n.pieces += c.pieces
		}
	}
	return n
}

// leaf wraps a piece in a node with a fresh priority.
func (t *treeOps) leaf(p piece) *node { return mk(nil, nil, t.nextPrio(), p) }

// merge joins a and b (all of a precedes all of b).
func (t *treeOps) merge(a, b *node) *node {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case a.prio >= b.prio:
		return mk(a.l, t.merge(a.r, b), a.prio, a.p)
	default:
		return mk(t.merge(a, b.l), b.r, b.prio, b.p)
	}
}

// split returns the first i lines of n and the rest. A piece that straddles i
// is cut in two; its halves get fresh priorities, which may exceed those of
// their ancestors, so the parts are rejoined with merge, not mk.
// n is not modified.
func (t *treeOps) split(n *node, i int) (l, r *node) {
	switch {
	case n == nil:
		return nil, nil
	case i <= 0:
		return nil, n
	case i >= n.lines:
		return n, nil
	}
	ls := n.l.size()
	pl := n.p.length()
	switch {
	case i <= ls:
		a, b := t.split(n.l, i)
		return a, t.merge(b, mk(nil, n.r, n.prio, n.p))
	case i >= ls+pl:
		a, b := t.split(n.r, i-ls-pl)
		return t.merge(mk(n.l, nil, n.prio, n.p), a), b
	}
	pa, pb := splitPiece(n.p, i-ls)
	return t.merge(n.l, t.leaf(pa)), t.merge(t.leaf(pb), n.r)
}

// at returns the piece holding line i of the tree and the line's index inside
// that piece. ok is false when i is outside the tree.
func at(n *node, i int) (p piece, idx int, ok bool) {
	if i < 0 || i >= n.size() {
		return piece{}, 0, false
	}
	for {
		ls := n.l.size()
		pl := n.p.length()
		switch {
		case i < ls:
			n = n.l
		case i < ls+pl:
			return n.p, i - ls, true
		default:
			i -= ls + pl
			n = n.r
		}
	}
}

// pieceKind tells literal lines from base lines.
type pieceKind uint8

const (
	kindBase    pieceKind = iota // base line, inside a run piece or in the tail
	kindLiteral                  // literal line
)

// pieceKindAt reports whether document line i is a literal line (for
// LineDirty). Lines past the tree are base lines of the tail.
func pieceKindAt(n *node, i int) pieceKind {
	if p, _, ok := at(n, i); ok && !p.isRun() {
		return kindLiteral
	}
	return kindBase
}

// cover makes the tree reach at least document line j (exclusive) by moving
// the needed base lines from the tail into a run piece at the end of the tree.
func (t *treeOps) cover(st bigState, j int) bigState {
	if j <= st.root.size() {
		return st
	}
	n := j - st.root.size()
	st.root = t.merge(st.root, t.leaf(runPiece(st.tailFirst, n)))
	st.tailFirst += n
	return st
}

// replace returns the state in which document lines [i, j) are replaced by
// repl. docLines is the number of lines of the document st describes; i and j
// are clamped to [0, docLines] with i <= j. The old state is untouched.
func (t *treeOps) replace(st bigState, docLines, i, j int, repl []litLine) bigState {
	j = min(max(j, 0), docLines)
	i = min(max(i, 0), j)
	st = t.cover(st, j)
	left, rest := t.split(st.root, i)
	_, right := t.split(rest, j-i)
	if len(repl) > 0 {
		left = t.merge(left, t.leaf(litPiece(repl)))
	}
	st.root = t.merge(left, right)
	return st
}
