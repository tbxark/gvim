package document

// forEachCaret runs step once per caret, in document order. step sees the caret
// as d.Cursor, d.Sel and d.want, and primary says whether it is the real primary
// caret. Carets that meet afterwards are merged.
func (d *Document) ForEachCaret(step func(primary bool)) {
	cs := d.AllCarets()
	for i := range cs {
		c := &cs[i]
		d.Cursor, d.Sel, d.Want = c.Head, c.Selection(), c.Want-1
		step(c.Primary)
		c.Anchor, c.Head, c.Want = d.Cursor, d.Cursor, d.Want+1
		if d.Sel != nil {
			c.Anchor = d.Sel.Anchor
		}
	}
	d.SetCarets(cs)
}
