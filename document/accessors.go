package document

// Accessors for the session half. Package editor reads and writes the state a
// document keeps besides its text through these methods; the fields stay
// unexported so the model keeps one owner. They are thin, and they exist so the
// session (tabs, save jobs, statuses, plugin seam) can drive a document without
// sharing its fields.

// DocSeam is the per-document plugin seam state: its id, buffer generation and
// change tracking. See docSeam.
type DocSeam = docSeam

// LargeDoc is the state of a document in large-file mode. See largeDoc.
type LargeDoc = largeDoc

// LargeSave is the large-document write state a SaveJob carries. See largeSave.
type LargeSave = largeSave

// Ext returns the plugin-facing state of d.
func (d *Document) Ext() *DocExt { return &d.ext }

// SetExt replaces the plugin-facing state of d.
func (d *Document) SetExt(e DocExt) { d.ext = e }

// Seam returns the plugin seam state of d.
func (d *Document) Seam() *DocSeam { return &d.seam }

// LargeState returns the large-file state of d, or nil when d is not large.
func (d *Document) LargeState() *LargeDoc { return d.large }

// SetLargeState replaces the large-file state of d.
func (d *Document) SetLargeState(l *LargeDoc) { d.large = l }

// View returns the display settings of d, for the view actions to change in place.
func (d *Document) View() *ViewSettings { return &d.view }

// SetView replaces the display settings of d.
func (d *Document) SetView(v ViewSettings) { d.view = v }

// SetReadOnly sets whether d refuses every change to its text and file.
func (d *Document) SetReadOnly(ro bool) { d.readOnly = ro }

// SetLanguage records the lexer name of d.
func (d *Document) SetLanguage(name string) { d.language = name }

// FoldedLines returns the start lines of the collapsed folds of d.
func (d *Document) FoldedLines() map[int]bool { return d.folded }

// SetFoldedLines replaces the collapsed folds of d.
func (d *Document) SetFoldedLines(folded map[int]bool) { d.folded = folded }

// SetPreview sets whether d is a preview tab.
func (d *Document) SetPreview(preview bool) { d.preview = preview }

// SetBinary sets whether d is a file that is not text.
func (d *Document) SetBinary(binary bool) { d.binary = binary }

// UntitledNumber returns N when d is the Untitled-N buffer, else 0.
func (d *Document) UntitledNumber() int { return d.untitled }

// SetUntitledNumber makes d the Untitled-N buffer.
func (d *Document) SetUntitledNumber(n int) { d.untitled = n }

// EncodingLabel returns the label of the encoding the saved text is written in.
// It is the stored label; Encoding returns the label a save would use now.
func (d *Document) EncodingLabel() string { return d.encoding }

// SetEncodingLabel records the encoding label of the saved text.
func (d *Document) SetEncodingLabel(label string) { d.encoding = label }

// LargeSave returns the large-document write state of j, or nil.
func (j SaveJob) LargeSave() *LargeSave { return j.large }

// Err returns the error that makes the job write nothing, or nil.
func (j SaveJob) Err() error { return j.err }

// WithErr returns r with err as its failure, for the callers that build a
// failed result by hand (the tests and the ApplySaves callers).
func (r SaveResult) WithErr(err error) SaveResult {
	r.err = err
	return r
}
