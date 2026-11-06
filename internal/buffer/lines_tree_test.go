package buffer

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

// docLine is the oracle's view of one document line. Run lines are identified
// by their base line number; literal lines by text and terminator flag.
type docLine struct {
	lit  bool
	base int
	text string
	eol  uint8
}

// treeDoc is a tree plus the number of base lines under it, the pair every
// replace needs.
type treeDoc struct {
	t         treeOps
	st        bigState
	baseLines int
}

func newTreeDoc(baseLines int) *treeDoc {
	return &treeDoc{baseLines: baseLines}
}

func (d *treeDoc) lines() int { return d.st.root.size() + d.baseLines - d.st.tailFirst }

func (d *treeDoc) replace(i, j int, repl []litLine) {
	d.st = d.t.replace(d.st, d.lines(), i, j, repl)
}

// flatten lists the document lines of st through at() and the virtual tail.
func flatten(st bigState, baseLines int) []docLine {
	var out []docLine
	for i := 0; i < st.root.size(); i++ {
		p, k, ok := at(st.root, i)
		if !ok {
			panic("at failed inside the tree")
		}
		if p.isRun() {
			out = append(out, docLine{base: p.first + k})
		} else {
			out = append(out, docLine{lit: true, text: string(p.lit[k]), eol: p.eolAt(k)})
		}
	}
	for b := st.tailFirst; b < baseLines; b++ {
		out = append(out, docLine{base: b})
	}
	return out
}

// checkTree verifies the structural invariants of the tree of st and returns
// the lines of the document it describes.
func checkTree(t *testing.T, st bigState, baseLines int) {
	t.Helper()
	type sums struct{ lines, runes, bytes int }
	var runEnd int // end of the previous run piece
	var walk func(n *node) sums
	walk = func(n *node) sums {
		if n == nil {
			return sums{}
		}
		if n.l != nil && n.l.prio > n.prio || n.r != nil && n.r.prio > n.prio {
			t.Fatalf("heap order violated at prio %d", n.prio)
		}
		l := walk(n.l)
		own := sums{lines: n.p.length()}
		if own.lines <= 0 {
			t.Fatalf("empty piece %+v", n.p)
		}
		if n.p.isRun() {
			if n.p.first < runEnd {
				t.Fatalf("run [%d,+%d) overlaps or precedes earlier run ending at %d", n.p.first, n.p.count, runEnd)
			}
			runEnd = n.p.first + n.p.count
			if len(n.p.nb) != 0 || len(n.p.eol) != 0 {
				t.Fatalf("run piece carries literal metadata: %+v", n.p)
			}
		} else {
			if len(n.p.nb) != len(n.p.lit) || n.p.eol != nil && len(n.p.eol) != len(n.p.lit) {
				t.Fatalf("lit/eol/nb out of lockstep: %d/%d/%d", len(n.p.lit), len(n.p.eol), len(n.p.nb))
			}
			for i, rs := range n.p.lit {
				own.runes += len(rs)
				own.bytes += len(string(rs))
				if n.p.nb[i] != len(string(rs)) {
					t.Fatalf("nb[%d]=%d, want %d", i, n.p.nb[i], len(string(rs)))
				}
			}
		}
		r := walk(n.r)
		want := sums{l.lines + own.lines + r.lines, l.runes + own.runes + r.runes, l.bytes + own.bytes + r.bytes}
		got := sums{n.lines, n.litRunes, n.litBytes}
		if got != want {
			t.Fatalf("summary %+v, want %+v", got, want)
		}
		return want
	}
	walk(st.root)
	var count func(n *node) (lit, pieces int)
	count = func(n *node) (lit, pieces int) {
		if n == nil {
			return 0, 0
		}
		ll, lp := count(n.l)
		rl, rp := count(n.r)
		lit, pieces = ll+rl+len(n.p.lit), lp+rp+1
		if n.litLines != lit || n.pieces != pieces {
			t.Fatalf("litLines/pieces %d/%d, want %d/%d", n.litLines, n.pieces, lit, pieces)
		}
		return lit, pieces
	}
	count(st.root)
	if runEnd > st.tailFirst {
		t.Fatalf("run reaches base line %d but tailFirst is %d", runEnd, st.tailFirst)
	}
	if st.tailFirst > baseLines {
		t.Fatalf("tailFirst %d beyond base lines %d", st.tailFirst, baseLines)
	}
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return 1 + max(height(n.l), height(n.r))
}

func shape(n *node) string {
	if n == nil {
		return "."
	}
	return fmt.Sprintf("(%s %d/%d:%d %s)", shape(n.l), n.p.first, n.p.length(), n.prio, shape(n.r))
}

var treeAlphabet = []rune("abcxyz é世🙂\t")

func randomLitLines(rng *rand.Rand, n int) []litLine {
	out := make([]litLine, n)
	for i := range out {
		rs := make([]rune, rng.Intn(6))
		for k := range rs {
			rs[k] = treeAlphabet[rng.Intn(len(treeAlphabet))]
		}
		out[i] = litLine{runes: rs, eol: uint8(rng.Intn(3))}
	}
	return out
}

func litModel(repl []litLine) []docLine {
	out := make([]docLine, len(repl))
	for i, l := range repl {
		out[i] = docLine{lit: true, text: string(l.runes), eol: l.eol}
	}
	return out
}

func TestTreeReplaceMatchesOracle(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		baseLines := 1 + rng.Intn(60)
		d := newTreeDoc(baseLines)
		model := make([]docLine, baseLines)
		for b := range model {
			model[b] = docLine{base: b}
		}
		type kept struct {
			st   bigState
			want []docLine
		}
		var history []kept
		for step := 0; step < 150; step++ {
			n := len(model)
			if d.lines() != n {
				t.Fatalf("seed %d step %d: lines()=%d, model has %d", seed, step, d.lines(), n)
			}
			i := rng.Intn(n + 1)
			j := i + rng.Intn(min(n-i, 8)+1)
			repl := randomLitLines(rng, rng.Intn(5))
			prev := d.st
			prevFlat := slices.Clone(model)

			d.replace(i, j, repl)
			model = slices.Concat(model[:i:i], litModel(repl), model[j:])

			got := flatten(d.st, baseLines)
			if !slices.Equal(got, model) {
				t.Fatalf("seed %d step %d replace(%d,%d,%d lines):\n got  %v\n want %v", seed, step, i, j, len(repl), got, model)
			}
			checkTree(t, d.st, baseLines)
			// The state before the edit must still describe the old document.
			if !slices.Equal(flatten(prev, baseLines), prevFlat) {
				t.Fatalf("seed %d step %d: replace changed the previous state", seed, step)
			}
			if step%10 == 0 {
				history = append(history, kept{d.st, slices.Clone(model)})
			}
		}
		for k, h := range history {
			if got := flatten(h.st, baseLines); !slices.Equal(got, h.want) {
				t.Fatalf("seed %d: kept state %d changed after later edits", seed, k)
			}
			checkTree(t, h.st, baseLines)
		}
	}
}

func TestTreeReplaceEdgeCases(t *testing.T) {
	lit := func(ss ...string) []litLine {
		out := make([]litLine, len(ss))
		for i, s := range ss {
			out[i] = litLine{runes: []rune(s)}
		}
		return out
	}
	tests := []struct {
		name      string
		baseLines int
		i, j      int
		repl      []litLine
		wantTail  int // tailFirst after the edit
		want      []string
	}{
		{"insert in empty tail", 3, 0, 0, lit("x"), 0, []string{"x", "#0", "#1", "#2"}},
		{"replace first line", 3, 0, 1, lit("x"), 1, []string{"x", "#1", "#2"}},
		{"replace last line covers all", 3, 2, 3, lit("x"), 3, []string{"#0", "#1", "x"}},
		{"replace middle", 5, 2, 3, lit("x", "y"), 3, []string{"#0", "#1", "x", "y", "#3", "#4"}},
		{"delete everything", 4, 0, 4, nil, 4, nil},
		{"delete range with empty repl", 4, 1, 3, nil, 3, []string{"#0", "#3"}},
		{"empty replace is a no-op", 4, 2, 2, nil, 2, []string{"#0", "#1", "#2", "#3"}},
		{"j past the end is clamped", 3, 1, 99, lit("x"), 3, []string{"#0", "x"}},
		{"negative i is clamped", 3, -5, 1, lit("x"), 1, []string{"x", "#1", "#2"}},
		{"i after j is clamped to j", 3, 3, 1, lit("x"), 1, []string{"#0", "x", "#1", "#2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newTreeDoc(tc.baseLines)
			d.replace(tc.i, tc.j, tc.repl)
			checkTree(t, d.st, tc.baseLines)
			var got []string
			for _, l := range flatten(d.st, tc.baseLines) {
				if l.lit {
					got = append(got, l.text)
				} else {
					got = append(got, fmt.Sprintf("#%d", l.base))
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("document %v, want %v", got, tc.want)
			}
			if d.lines() != len(tc.want) {
				t.Errorf("lines() = %d, want %d", d.lines(), len(tc.want))
			}
		})
	}
}

func TestTreeCoverAdvancesTail(t *testing.T) {
	d := newTreeDoc(100)
	d.replace(10, 11, []litLine{{runes: []rune("x")}})
	if d.st.tailFirst != 11 {
		t.Fatalf("tailFirst = %d, want 11", d.st.tailFirst)
	}
	if d.st.root.lines != 11 {
		t.Fatalf("tree covers %d lines, want 11", d.st.root.lines)
	}
	// An edit inside the covered part does not touch the tail.
	d.replace(2, 3, []litLine{{runes: []rune("y")}})
	if d.st.tailFirst != 11 {
		t.Fatalf("tailFirst = %d after an edit inside the tree, want 11", d.st.tailFirst)
	}
	// Far edit covers the gap with exactly one extra run.
	d.replace(50, 51, []litLine{{runes: []rune("z")}})
	if d.st.tailFirst != 51 {
		t.Fatalf("tailFirst = %d, want 51", d.st.tailFirst)
	}
	if d.lines() != 100 {
		t.Fatalf("lines() = %d, want 100", d.lines())
	}
	checkTree(t, d.st, 100)
	if k := pieceKindAt(d.st.root, 50); k != kindLiteral {
		t.Errorf("line 50 kind = %v, want literal", k)
	}
}

func TestTreeSplitAtPieceEdges(t *testing.T) {
	// run 0..2 | lit a,b | run 3..4 | lit c | tail from 5
	d := newTreeDoc(8)
	d.replace(0, 0, nil)
	d.replace(3, 3, []litLine{{runes: []rune("a")}, {runes: []rune("b")}})
	d.replace(7, 7, []litLine{{runes: []rune("c")}})
	checkTree(t, d.st, 8)
	whole := flatten(d.st, 8)
	root := d.st.root
	for i := 0; i <= root.size(); i++ {
		l, r := d.t.split(root, i)
		if l.size() != i || r.size() != root.size()-i {
			t.Fatalf("split(%d) sizes %d/%d, tree has %d", i, l.size(), r.size(), root.size())
		}
		joined := flatten(bigState{root: d.t.merge(l, r), tailFirst: d.st.tailFirst}, 8)
		if !slices.Equal(joined, whole) {
			t.Fatalf("split(%d)+merge changed the document:\n got  %v\n want %v", i, joined, whole)
		}
		checkTree(t, bigState{root: l, tailFirst: d.st.tailFirst}, 8)
		checkTree(t, bigState{root: r, tailFirst: d.st.tailFirst}, 8)
	}
	if !slices.Equal(flatten(d.st, 8), whole) {
		t.Fatal("split changed the original tree")
	}
	// Empty and nil trees.
	if l, r := d.t.split(nil, 3); l != nil || r != nil {
		t.Errorf("split(nil) = %v, %v", l, r)
	}
	if got := d.t.merge(nil, nil); got != nil {
		t.Errorf("merge(nil, nil) = %v", got)
	}
	if got := d.t.merge(root, nil); got != root {
		t.Error("merge(x, nil) should return x")
	}
	if got := d.t.merge(nil, root); got != root {
		t.Error("merge(nil, x) should return x")
	}
	if l, r := d.t.split(root, -1); l != nil || r != root {
		t.Error("split(-1) should return (nil, root)")
	}
	if l, r := d.t.split(root, root.size()+5); l != root || r != nil {
		t.Error("split(past end) should return (root, nil)")
	}
}

func TestTreeAtOutOfRange(t *testing.T) {
	d := newTreeDoc(3)
	d.replace(0, 1, []litLine{{runes: []rune("x")}})
	for _, i := range []int{-1, d.st.root.size(), 99} {
		if _, _, ok := at(d.st.root, i); ok {
			t.Errorf("at(%d) ok, want false", i)
		}
	}
	if _, _, ok := at(nil, 0); ok {
		t.Error("at(nil, 0) ok, want false")
	}
}

func TestPieceKindAt(t *testing.T) {
	d := newTreeDoc(6)
	d.replace(1, 2, []litLine{{runes: []rune("x")}, {runes: []rune("y")}}) // doc: #0 x y #2 #3 #4 #5
	want := []pieceKind{kindBase, kindLiteral, kindLiteral, kindBase, kindBase, kindBase, kindBase}
	for i, w := range want {
		if got := pieceKindAt(d.st.root, i); got != w {
			t.Errorf("pieceKindAt(%d) = %v, want %v", i, got, w)
		}
	}
	if pieceKindAt(nil, 0) != kindBase {
		t.Error("an empty tree has no literal lines")
	}
}

func TestPieceEolSplitMergeLockstep(t *testing.T) {
	lines := []litLine{
		{[]rune("a"), eolLF},
		{[]rune("b"), eolDefault},
		{[]rune("é"), eolCRLF},
		{nil, eolCRLF},
		{[]rune("世e"), eolDefault},
	}
	p := litPiece(lines)
	if p.length() != len(lines) {
		t.Fatalf("length = %d", p.length())
	}
	for k := 1; k < len(lines); k++ {
		a, b := splitPiece(p, k)
		for part, off, pc := 0, 0, a; part < 2; part, off, pc = part+1, k, b {
			if len(pc.lit) != len(pc.nb) || pc.eol != nil && len(pc.eol) != len(pc.lit) {
				t.Fatalf("k=%d part %d out of lockstep: %d/%d/%d", k, part, len(pc.lit), len(pc.eol), len(pc.nb))
			}
			for i := range pc.lit {
				want := lines[off+i]
				if string(pc.lit[i]) != string(want.runes) || pc.eolAt(i) != want.eol || pc.nb[i] != len(string(want.runes)) {
					t.Fatalf("k=%d part %d line %d = %q/%d/%d, want %q/%d", k, part, i, string(pc.lit[i]), pc.eolAt(i), pc.nb[i], string(want.runes), want.eol)
				}
			}
		}
		// Capacity is clipped: appending to the left half must not write into the right half.
		a.lit = append(a.lit, []rune("CLOBBER"))
		a.eol = append(a.eol, eolLF)
		a.nb = append(a.nb, 7)
		if string(b.lit[0]) != string(lines[k].runes) || b.eolAt(0) != lines[k].eol || b.nb[0] != len(string(lines[k].runes)) {
			t.Fatalf("k=%d: append to the left half overwrote the right half", k)
		}
	}

	// Through the tree: split anywhere, merge back, the (text, eol) sequence is unchanged.
	var tr treeOps
	root := tr.leaf(p)
	for i := 0; i <= len(lines); i++ {
		l, r := tr.split(root, i)
		got := flatten(bigState{root: tr.merge(l, r)}, 0)
		for k, g := range got {
			if g.text != string(lines[k].runes) || g.eol != lines[k].eol {
				t.Fatalf("split(%d)+merge line %d = %+v", i, k, g)
			}
		}
		if len(got) != len(lines) {
			t.Fatalf("split(%d)+merge has %d lines, want %d", i, len(got), len(lines))
		}
	}

	// An all-default piece keeps eol nil, also after a split.
	q := litPiece([]litLine{{runes: []rune("a")}, {runes: []rune("b")}})
	if q.eol != nil {
		t.Error("all-default literal piece allocated an eol slice")
	}
	if a, b := splitPiece(q, 1); a.eol != nil || b.eol != nil {
		t.Error("split of an all-default piece allocated an eol slice")
	}
}

func TestLitBytesSummaryInvariant(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	d := newTreeDoc(30)
	want := 0 // bytes of literal lines currently in the tree
	var model []docLine
	for b := 0; b < 30; b++ {
		model = append(model, docLine{base: b})
	}
	for step := 0; step < 100; step++ {
		n := len(model)
		i := rng.Intn(n + 1)
		j := i + rng.Intn(min(n-i, 5)+1)
		repl := randomLitLines(rng, rng.Intn(4))
		if step%7 == 0 {
			// Invalid runes encode as U+FFFD: 3 bytes.
			repl = append(repl, litLine{runes: []rune{'a', -1, 0xD800, 0x110000}})
		}
		d.replace(i, j, repl)
		model = slices.Concat(model[:i:i], litModel(repl), model[j:])
		want = 0
		for _, l := range model {
			if l.lit {
				want += len(l.text)
			}
		}
		if d.st.root.litBytes != want {
			t.Fatalf("step %d: litBytes %d, want %d", step, d.st.root.litBytes, want)
		}
		checkTree(t, d.st, 30)
	}
	runes := 0
	for _, l := range model {
		if l.lit {
			runes += len([]rune(l.text))
		}
	}
	if d.st.root.litRunes != runes {
		t.Errorf("litRunes %d, want %d", d.st.root.litRunes, runes)
	}
}

func TestLitPieceByteLength(t *testing.T) {
	tests := []struct {
		name string
		in   []rune
		want int
	}{
		{"empty", nil, 0},
		{"ascii", []rune("abc"), 3},
		{"two byte", []rune("é"), 2},
		{"three byte", []rune("世"), 3},
		{"four byte", []rune("🙂"), 4},
		{"surrogate counts as FFFD", []rune{0xD800}, 3},
		{"out of range counts as FFFD", []rune{0x110000, -1}, 6},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := litPiece([]litLine{{runes: tc.in}})
			if p.nb[0] != tc.want {
				t.Errorf("nb = %d, want %d", p.nb[0], tc.want)
			}
			if got := len(string(tc.in)); got != tc.want {
				t.Errorf("len(string(runes)) = %d, want %d: summary would disagree with encoding", got, tc.want)
			}
		})
	}
}

func TestTreeLiteralsShareBackingArray(t *testing.T) {
	lines := make([]litLine, 8)
	for i := range lines {
		lines[i] = litLine{runes: []rune(strings.Repeat("x", i+1))}
	}
	p := litPiece(lines)
	a, b := splitPiece(p, 3)
	if &a.lit[0] != &p.lit[0] || &b.lit[0] != &p.lit[3] {
		t.Error("split halves should alias the original lit array")
	}
	if &a.lit[0][0] != &p.lit[0][0] {
		t.Error("split must not copy the rune slices")
	}
	if cap(a.lit) != 3 || cap(b.lit) != 5 {
		t.Errorf("capacities %d/%d, want 3/5", cap(a.lit), cap(b.lit))
	}
}

func TestTreeShapeIsDeterministicAndBalanced(t *testing.T) {
	build := func() *treeDoc {
		d := newTreeDoc(5000)
		// Successive edits at ascending and descending lines are the usual
		// worst case for an unbalanced tree.
		for i := 0; i < 1000; i++ {
			d.replace(i*2, i*2+1, []litLine{{runes: []rune("x")}})
		}
		for i := 999; i >= 0; i-- {
			d.replace(i, i, []litLine{{runes: []rune("y")}})
		}
		return d
	}
	a, b := build(), build()
	if shape(a.st.root) != shape(b.st.root) {
		t.Fatal("the same operation sequence produced different trees")
	}
	checkTree(t, a.st, 5000)
	if h := height(a.st.root); h > 60 {
		t.Errorf("height %d for %d pieces; treap is not balancing", h, a.st.root.lines)
	}
	if a.lines() != 5000+1000 {
		t.Errorf("lines() = %d, want 6000", a.lines())
	}
}
