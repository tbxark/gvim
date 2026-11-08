package editor

// A document in large file mode holds a file descriptor and caches that belong to its
// buffer. Dropping the document from the editor does not free them: the buffer must be
// closed. releaseDoc is the one place that does it, and every path that removes a
// document, or replaces it, calls it.

// releaseDoc ends the life of a document that leaves the editor: a closed tab, a
// preview that another file replaces, a placeholder that a file replaces, and a document
// that was loaded but not opened because its file is already open. It drops the work
// that waits for the line index, cancels the background scan, and closes the buffer,
// which releases the file. It is safe to call more than once, and on a document that
// is not large.
//
// A scan whose job is running (inFlight) is not closed here, because the job is
// reading from it on another goroutine. The scan is forgotten instead, so ApplyIndex
// drops the job's result and closes the scan then.
//
// The document is not changed otherwise: a caller that still holds it sees a closed
// buffer, whose lines read as empty.
func (e *Editor) releaseDoc(d *Document) {
	if d == nil {
		return
	}
	if l := d.large; l != nil {
		l.pending = nil
		if !l.inFlight {
			l.scan.Close()
		}
		l.scan = nil
	}
	_ = d.Buffer.Close()
}

// Close releases the files of every open document. The editor keeps its documents, so
// the caller can still read their text, for the session, before it calls Close; it
// calls Close once, when the program ends. It is safe to call more than once.
func (e *Editor) Close() {
	for _, d := range e.docs {
		e.releaseDoc(d)
	}
}
