// Package sidebar is the left column of the frame: the file explorer and the
// sections stacked under it. Each section is a header row over a body; adjacent
// sections share one border row, which is the lower section's header, so a
// boundary costs one row and not two. With one section the column is the box the
// explorer has always been: the same cells, in the same places.
//
// The package owns the section geometry (Layout), the header rows (Header,
// PaintHeader) and the Contents section's body (Contents). The explorer body is
// painted by the explorer panel; the column composes the parts.
//
// The column's rows:
//
//	┌ ▼ File Explorer ─────⟳─×─┐   section 0 header, the column's top border
//	│▼ demo                    │   section 0 body: two walls and its rows
//	│  ▼ crates                │
//	├ ▼ Contents ────────────×─┤   section 1 header, shared with section 0
//	│▼ Interface               │   section 1 body
//	└──────────────────────────┘   the column's own bottom border
//
// Empty space under the last open section (every section collapsed) is ground:
// it is filled with the column's background and has no walls.
package sidebar

// Kind names what a section of the column holds.
type Kind int

const (
	// KindExplorer is the file tree, the first section.
	KindExplorer Kind = iota
	// KindContents is the Markdown outline, stacked under the explorer while it
	// is open.
	KindContents
)

// Spec is one section's state for the layout: the body rows it asks for and
// whether the reader collapsed it. The explorer asks for no rows (0), so the
// fixed sections take theirs and it shares the remainder.
type Spec struct {
	Kind      Kind
	Rows      int  // body rows asked for; 0 shares the column's remainder
	Collapsed bool // the reader collapsed the section
	// Title is the header title of a plugin section; "" derives the title from the kind
	// (the native sections' headers).
	Title string
	// NoClose hides the header's close button (a plugin section that is not closable).
	NoClose bool
}

// Section is where one section of the column landed. HeaderY is its header row,
// which is the column's top border for the first section and the shared row with
// the section above for every other. BodyTop and BodyBottom are its body rows,
// both inclusive; a collapsed section has BodyBottom = BodyTop-1. Collapsed is
// the effective state: the reader's collapse, or the squeeze of a column that is
// too short for every section.
type Section struct {
	Kind       Kind
	Index      int
	HeaderY    int
	BodyTop    int
	BodyBottom int
	Collapsed  bool
	// Title and NoClose are the Spec's plugin-section header values; empty/false for the
	// native sections.
	Title   string
	NoClose bool
}

// Rows is the height of the section's body, zero when it is collapsed.
func (s Section) Rows() int {
	if s.BodyBottom < s.BodyTop {
		return 0
	}
	return s.BodyBottom - s.BodyTop + 1
}

// Layout places specs in the column from row top to row bottom, where bottom is
// the column's bottom border row (inclusive). Body rows go to the sections: a
// section that asked for rows is fixed at that many, and the sections that asked
// for none share the remainder. A collapsed section gives its rows to the open
// ones. When the column cannot fit every open section one body row, sections
// collapse from the bottom up (the tree stays), and when a fixed section would
// leave a sharing section without its row, the fixed ones shrink from the bottom
// up, resolved for the frame rather than rewritten: a later, taller frame gets
// the requested rows back.
//
// A column too short even for the header rows plus its bottom border shows the
// top sections only. The result is pure geometry: the painter and the hit map
// both read it, so they cannot drift apart.
func Layout(top, bottom int, specs []Spec) []Section {
	if len(specs) == 0 || bottom < top {
		return nil
	}
	height := bottom - top + 1
	shown := specs
	if height < len(shown)+1 {
		// The column keeps the top header and gives the rows above the bottom
		// border to the following sections, in order.
		shown = shown[:max(height-1, 0)]
	}
	n := len(shown)
	if n == 0 {
		return nil
	}
	body := height - n - 1

	squeezed := make([]bool, n)
	open := 0
	for i, s := range shown {
		squeezed[i] = s.Collapsed
		if !squeezed[i] {
			open++
		}
	}
	for open > body {
		last := -1
		for i := n - 1; i >= 0; i-- {
			if !squeezed[i] {
				last = i
				break
			}
		}
		if last < 0 {
			break
		}
		squeezed[last] = true
		open--
	}

	rows := distribute(body, shown, squeezed)
	out := make([]Section, n)
	y := top
	for i, s := range shown {
		out[i] = Section{Kind: s.Kind, Index: i, HeaderY: y, BodyTop: y + 1, BodyBottom: y, Collapsed: squeezed[i], Title: s.Title, NoClose: s.NoClose}
		y++
		if r := rows[i]; r > 0 {
			out[i].BodyTop = y
			out[i].BodyBottom = y + r - 1
			y += r
		}
	}
	return out
}

// distribute returns the body rows per section for a column with body rows to
// give. Collapsed sections get none. A section with a requested extent is fixed
// at it and the rest share the remainder; when every open section asked for
// rows the last open one is the remainder instead, so the column is exactly
// filled. Fixed sections shrink from the bottom up when they would leave a
// sharing section without its one row.
func distribute(body int, specs []Spec, collapsed []bool) []int {
	rows := make([]int, len(specs))
	if body <= 0 {
		return rows
	}
	var open []int
	for i := range specs {
		if !collapsed[i] {
			open = append(open, i)
		}
	}
	if len(open) == 0 {
		return rows
	}
	var flex []int
	for _, i := range open {
		if specs[i].Rows <= 0 {
			flex = append(flex, i)
		}
	}
	if len(flex) == 0 {
		// Everyone asked for rows: the last open section takes the remainder.
		flex = []int{open[len(open)-1]}
	}
	fixed := make(map[int]int, len(open)-len(flex))
	for _, i := range open {
		isFlex := false
		for _, f := range flex {
			if f == i {
				isFlex = true
				break
			}
		}
		if !isFlex {
			fixed[i] = max(specs[i].Rows, 1)
		}
	}
	// Fixed sections give way bottom-up, but each keeps one row.
	sum := 0
	for _, r := range fixed {
		sum += r
	}
	budget := body - len(flex)
	order := make([]int, 0, len(fixed))
	for _, i := range open {
		if _, ok := fixed[i]; ok {
			order = append(order, i)
		}
	}
	for k := len(order) - 1; k >= 0 && sum > budget; k-- {
		i := order[k]
		give := min(sum-budget, fixed[i]-1)
		fixed[i] -= give
		sum -= give
	}
	remain := max(body-sum, 0)
	share, extra := remain/len(flex), remain%len(flex)
	for k, i := range flex {
		rows[i] = share
		if k == len(flex)-1 {
			rows[i] += extra
		}
	}
	for i, r := range fixed {
		rows[i] = r
	}
	return rows
}
