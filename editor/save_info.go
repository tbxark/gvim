package editor

// Written reports what a finished save wrote: the file and the document it came from.
// ok is false for a save that failed, and for a large document's save that wrote
// nothing because the file was already as the document left it. The root raises
// after_file_save from it, once ApplySaves has recorded the result.
func (r SaveResult) Written() (path string, doc DocID, ok bool) {
	if r.err != nil || r.skipped || r.job.doc == nil {
		return "", 0, false
	}
	return r.job.path, r.job.doc.ID(), true
}
