package decor

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// mirror is a text with the offsets of anchors in it. It is the oracle of the anchor
// arithmetic: where the store works in lines and byte columns, the mirror works in byte
// offsets of the whole text, which is a different computation of the same movement.
type mirror struct{ text string }

// lineStart returns the offset of the start of a line.
func (m mirror) lineStart(line int) int {
	off := 0
	for range line {
		i := strings.IndexByte(m.text[off:], '\n')
		if i < 0 {
			return off
		}
		off += i + 1
	}
	return off
}

// lineEnd returns the offset of the line break that ends the line, or -1 for the last line.
func (m mirror) lineEnd(line int) int {
	i := strings.IndexByte(m.text[m.lineStart(line):], '\n')
	if i < 0 {
		return -1
	}
	return m.lineStart(line) + i
}

func (m mirror) lineOf(off int) int { return strings.Count(m.text[:off], "\n") }

func (m mirror) posOf(off int) (line, col int) {
	line = m.lineOf(off)
	return line, off - m.lineStart(line)
}

func (m mirror) offOf(line, col int) int { return m.lineStart(line) + col }

// mapOff moves an offset through "delete [a, b), then insert n bytes at a".
func mapOff(p, a, b, n int, right bool) int {
	if b > a && p > a {
		if p < b {
			p = a
		} else {
			p -= b - a
		}
	}
	if n > 0 && (p > a || (p == a && right)) {
		p += n
	}
	return p
}

// oracleItem is an item in the mirror's terms.
type oracleItem struct {
	it    Item
	alive bool
}

// step applies the edit to the oracle items and the text.
func (m *mirror) step(items []oracleItem, a, b int, text string) {
	for i := range items {
		o := &items[i]
		if !o.alive {
			continue
		}
		it := &o.it
		switch {
		case it.Kind == Highlight, it.Kind == Scroll && (it.ToLine != it.FromLine || it.ToCol != it.FromCol):
			from, to := m.offOf(it.FromLine, it.FromCol), m.offOf(it.ToLine, it.ToCol)
			from, to = mapOff(from, a, b, len(text), true), mapOff(to, a, b, len(text), false)
			if to <= from {
				o.alive = false
				continue
			}
			it.FromLine, it.FromCol = lineColAfter(*m, a, b, text, from)
			it.ToLine, it.ToCol = lineColAfter(*m, a, b, text, to)
		case it.Kind == Replace:
			p := m.offOf(it.FromLine, it.FromCol)
			if b > a && p >= a && p < b {
				o.alive = false
				continue
			}
			p = mapOff(p, a, b, len(text), true)
			it.FromLine, it.FromCol = lineColAfter(*m, a, b, text, p)
			it.ToLine, it.ToCol = it.FromLine, it.FromCol
		default: // anchored to the start of a line
			start := m.lineStart(it.FromLine)
			if end := m.lineEnd(it.FromLine); b > a && a <= start && end >= 0 && b > end {
				o.alive = false
				continue
			}
			p := mapOff(start, a, b, len(text), true)
			it.FromLine, _ = lineColAfter(*m, a, b, text, p)
			it.FromCol, it.ToLine, it.ToCol = 0, it.FromLine, 0
		}
	}
	m.text = m.text[:a] + text + m.text[b:]
}

// lineColAfter converts an offset of the text after the edit to a line and column.
func lineColAfter(m mirror, a, b int, text string, off int) (int, int) {
	after := mirror{text: m.text[:a] + text + m.text[b:]}
	return after.posOf(off)
}

func randText(r *rand.Rand, n int) string {
	const alphabet = "ab\né\n x"
	var sb strings.Builder
	runes := []rune(alphabet)
	for range n {
		sb.WriteRune(runes[r.IntN(len(runes))])
	}
	return sb.String()
}

// runeBoundaries returns the offsets of text at which a rune starts, and the end.
func runeBoundaries(text string) []int {
	var out []int
	for i := range text {
		out = append(out, i)
	}
	return append(out, len(text))
}

func TestApplyMatchesOffsetOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for round := range 300 {
		m := mirror{text: randText(r, 5+r.IntN(60))}
		s := New()
		var items []oracleItem
		var set []Item
		for range 12 {
			bounds := runeBoundaries(m.text)
			pick := func() (int, int) { return m.posOf(bounds[r.IntN(len(bounds))]) }
			var it Item
			switch r.IntN(5) {
			case 0, 1:
				fl, fc := pick()
				tl, tc := pick()
				it = Item{Kind: Highlight, FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc}
			case 2:
				fl, fc := pick()
				it = Item{Kind: Replace, FromLine: fl, FromCol: fc, Text: "r"}
			case 3:
				fl, _ := pick()
				it = Item{Kind: []Kind{Line, EOL, Gutter}[r.IntN(3)], FromLine: fl}
			default:
				fl, _ := pick()
				tl, _ := pick()
				it = Item{Kind: Scroll, FromLine: fl, ToLine: tl}
			}
			n, ok := normalize(it)
			if !ok {
				continue
			}
			set = append(set, n)
			items = append(items, oracleItem{it: n, alive: true})
		}
		if err := s.Set(1, "p", "a", set); err != nil {
			t.Fatal(err)
		}
		for e := range 25 {
			bounds := runeBoundaries(m.text)
			a := bounds[r.IntN(len(bounds))]
			b := a
			if r.IntN(2) == 0 {
				b = bounds[r.IntN(len(bounds))]
				if b < a {
					a, b = b, a
				}
			}
			var text string
			if r.IntN(3) > 0 {
				text = randText(r, r.IntN(6))
			}
			fl, fc := m.posOf(a)
			tl, tc := m.posOf(b)
			edit := Edit{FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc, Text: text}
			m.step(items, a, b, text)
			s.Apply(1, []Edit{edit}, false)

			var want []Item
			for _, o := range items {
				if o.alive {
					want = append(want, o.it)
				}
			}
			got := itemsOf(s, 1, "p", "a")
			sortItems := func(x []Item) {
				slices.SortStableFunc(x, func(p, q Item) int {
					return strings.Compare(itemKey(p), itemKey(q))
				})
			}
			sortItems(got)
			sortItems(want)
			if !slices.Equal(got, want) {
				t.Fatalf("round %d edit %d %+v:\n got %s\nwant %s\ntext %q", round, e, edit, brief(got), brief(want), m.text)
			}
		}
	}
}

func itemKey(it Item) string {
	return fmt.Sprintf("%d|%06d|%06d|%06d|%06d", it.Kind, it.FromLine, it.FromCol, it.ToLine, it.ToCol)
}

// naiveApply moves every item through the edit, without the shortcuts of group.apply.
func naiveApply(items []entry, sh shape) []entry {
	var out []entry
	for _, e := range items {
		if e.move(sh) {
			out = append(out, e)
		}
	}
	return out
}

func TestApplyShortcutsMatchFullPass(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 5))
	for round := range 200 {
		s := New()
		var set []Item
		for range 80 {
			line := r.IntN(60)
			switch r.IntN(3) {
			case 0:
				set = append(set, Item{Kind: Highlight, FromLine: line, FromCol: r.IntN(5), ToLine: line + r.IntN(6), ToCol: r.IntN(9) + 1})
			case 1:
				set = append(set, Item{Kind: Gutter, FromLine: line})
			default:
				set = append(set, Item{Kind: Replace, FromLine: line, FromCol: r.IntN(9)})
			}
		}
		_ = s.Set(1, "p", "a", set)
		g := s.docs[1].group("p", "a")
		g.sort()
		ref := slices.Clone(g.items)
		for e := range 12 {
			edit := Edit{FromLine: r.IntN(65), FromCol: r.IntN(6), Text: randText(r, r.IntN(4))}
			edit.ToLine, edit.ToCol = edit.FromLine, edit.FromCol
			if r.IntN(2) == 0 {
				edit.ToLine, edit.ToCol = edit.FromLine+r.IntN(4), r.IntN(8)
				if edit.ToLine == edit.FromLine && edit.ToCol < edit.FromCol {
					edit.ToCol = edit.FromCol
				}
			}
			sh := shapeOf(edit)
			ref = naiveApply(ref, sh)
			s.Apply(1, []Edit{edit}, false)
			var got []entry
			if d := s.docs[1]; d != nil {
				if gg := d.group("p", "a"); gg != nil {
					got = gg.items
				}
			}
			byID := func(a, b entry) int { return a.id - b.id }
			got = slices.SortedFunc(slices.Values(got), byID)
			ref = slices.SortedFunc(slices.Values(ref), byID)
			if !slices.EqualFunc(got, ref, func(a, b entry) bool { return a.Item == b.Item && a.id == b.id }) {
				t.Fatalf("round %d edit %d %+v: shortcut and full pass differ\n got %d items\nwant %d items", round, e, edit, len(got), len(ref))
			}
		}
	}
}

// brief lists items by kind and position, for failure messages.
func brief(items []Item) string {
	var sb strings.Builder
	for _, it := range items {
		fmt.Fprintf(&sb, "[%d (%d,%d)-(%d,%d)] ", it.Kind, it.FromLine, it.FromCol, it.ToLine, it.ToCol)
	}
	return sb.String()
}
