package app

// ReadOnly reports whether the document refuses edits, for the [RO] mark of the
// status row.
func (d documentAdapter) ReadOnly() bool { return d.doc.ReadOnly() }
